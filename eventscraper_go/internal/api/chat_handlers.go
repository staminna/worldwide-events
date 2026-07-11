package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"

	"github.com/jorgenunes/eventscraper/internal/chat"
	"github.com/jorgenunes/eventscraper/internal/store"
)

// Anonymous chat identity: register once with a display name, get an opaque
// bearer token, keep it on-device. Possession of the token is the identity —
// no passwords, no OAuth (v1).

type chatUserCtxKey struct{}

func chatUserFrom(r *http.Request) store.ChatUser {
	u, _ := r.Context().Value(chatUserCtxKey{}).(store.ChatUser)
	return u
}

// requireChatUser mirrors requireAdmin but resolves a per-user token against
// chat_users instead of comparing to a single shared secret.
func (s *Server) requireChatUser(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if tok == "" {
			writeErr(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		u, ok, err := s.store.GetChatUserByToken(r.Context(), tok)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "auth lookup failed")
			return
		}
		if !ok {
			writeErr(w, http.StatusUnauthorized, "invalid token")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), chatUserCtxKey{}, u)))
	}
}

func randHex(nBytes int) string {
	b := make([]byte, nBytes)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// inviteCode returns 6 chars from an alphabet without 0/O/1/I lookalikes.
func inviteCode() string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b)
}

// --- JSON shapes ---

type chatGroupJSON struct {
	ID            string `json:"id"`
	Type          string `json:"type"`
	EventID       string `json:"eventId,omitempty"`
	Name          string `json:"name"`
	InviteCode    string `json:"inviteCode,omitempty"`
	MemberCount   int    `json:"memberCount"`
	LastMessage   string `json:"lastMessage,omitempty"`
	LastMessageAt string `json:"lastMessageAt,omitempty"`
	CreatedAt     string `json:"createdAt"`
}

func toGroupJSON(g store.ChatGroup) chatGroupJSON {
	out := chatGroupJSON{
		ID: g.ID, Type: g.Type, EventID: g.EventID, Name: g.Name,
		InviteCode: g.InviteCode, MemberCount: g.MemberCount,
		LastMessage: g.LastMsgBody,
		CreatedAt:   g.CreatedAt.UTC().Format(time.RFC3339),
	}
	if !g.LastMsgAt.IsZero() {
		out.LastMessageAt = g.LastMsgAt.UTC().Format(time.RFC3339)
	}
	return out
}

type chatMessageJSON struct {
	ID        int64  `json:"id"`
	GroupID   string `json:"groupId"`
	UserID    string `json:"userId"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Body      string `json:"body"`
	CreatedAt string `json:"createdAt"`
}

func toMessageJSON(m store.ChatMessage) chatMessageJSON {
	return chatMessageJSON{
		ID: m.ID, GroupID: m.GroupID, UserID: m.UserID, Name: m.UserName,
		Kind: m.Kind, Body: m.Body,
		CreatedAt: m.CreatedAt.UTC().Format(time.RFC3339),
	}
}

// --- handlers ---

func (s *Server) handleChatRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || utf8.RuneCountInString(name) > 32 {
		writeErr(w, http.StatusBadRequest, "name must be 1-32 characters")
		return
	}
	u := store.ChatUser{
		ID:        "u" + randHex(8),
		Name:      name,
		Token:     randHex(32),
		CreatedAt: time.Now(),
	}
	if err := s.store.CreateChatUser(r.Context(), u); err != nil {
		writeErr(w, http.StatusInternalServerError, "could not create user")
		return
	}
	writeJSON(w, http.StatusCreated, envelope{Data: map[string]string{
		"id": u.ID, "name": u.Name, "token": u.Token,
	}, Meta: meta{Total: 1}})
}

func (s *Server) handleChatMyGroups(w http.ResponseWriter, r *http.Request) {
	u := chatUserFrom(r)
	groups, err := s.store.ListGroupsForUser(r.Context(), u.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not list groups")
		return
	}
	out := make([]chatGroupJSON, 0, len(groups))
	for _, g := range groups {
		out = append(out, toGroupJSON(g))
	}
	writeJSON(w, http.StatusOK, envelope{Data: out, Meta: meta{Total: len(out)}})
}

func (s *Server) handleChatCreateGroup(w http.ResponseWriter, r *http.Request) {
	u := chatUserFrom(r)
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || utf8.RuneCountInString(name) > 64 {
		writeErr(w, http.StatusBadRequest, "name must be 1-64 characters")
		return
	}
	g := store.ChatGroup{
		ID: "g" + randHex(8), Type: "private", Name: name,
		CreatedBy: u.ID, CreatedAt: time.Now(),
	}
	// Retry a few times in case the 6-char invite code collides (32^6 ≈ 1e9,
	// so in practice the first attempt wins).
	var err error
	for range 5 {
		g.InviteCode = inviteCode()
		if err = s.store.CreateGroup(r.Context(), g); err == nil {
			break
		}
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not create group")
		return
	}
	s.joinAndAnnounce(r.Context(), g, u)
	g.MemberCount = 1
	writeJSON(w, http.StatusCreated, envelope{Data: toGroupJSON(g), Meta: meta{Total: 1}})
}

func (s *Server) handleChatJoinByCode(w http.ResponseWriter, r *http.Request) {
	u := chatUserFrom(r)
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	code := strings.ToUpper(strings.TrimSpace(req.Code))
	g, ok, err := s.store.GetGroupByInvite(r.Context(), code)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if !ok {
		writeErr(w, http.StatusNotFound, "no group with that invite code")
		return
	}
	s.joinAndAnnounce(r.Context(), g, u)
	writeJSON(w, http.StatusOK, envelope{Data: toGroupJSON(g), Meta: meta{Total: 1}})
}

// handleChatJoinEventRoom lazily creates the public room of an event on
// first join, named after the event so the groups list reads naturally.
func (s *Server) handleChatJoinEventRoom(w http.ResponseWriter, r *http.Request) {
	u := chatUserFrom(r)
	eventID := chi.URLParam(r, "id")
	ev, found, err := s.store.GetEvent(r.Context(), eventID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "event lookup failed")
		return
	}
	if !found {
		writeErr(w, http.StatusNotFound, "event not found")
		return
	}
	g, err := s.store.GetOrCreateEventGroup(r.Context(), store.ChatGroup{
		ID: "g" + randHex(8), Type: "event", EventID: ev.ID,
		Name: ev.Title, CreatedAt: time.Now(),
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not open event room")
		return
	}
	s.joinAndAnnounce(r.Context(), g, u)
	writeJSON(w, http.StatusOK, envelope{Data: toGroupJSON(g), Meta: meta{Total: 1}})
}

func (s *Server) handleChatLeaveGroup(w http.ResponseWriter, r *http.Request) {
	u := chatUserFrom(r)
	groupID := chi.URLParam(r, "id")
	if err := s.store.LeaveGroup(r.Context(), groupID, u.ID); err != nil {
		writeErr(w, http.StatusInternalServerError, "could not leave group")
		return
	}
	s.hub.Broadcast(groupID, chat.Envelope{Type: chat.TypeLeave, UserID: u.ID, Name: u.Name})
	writeJSON(w, http.StatusOK, envelope{Data: map[string]bool{"left": true}, Meta: meta{Total: 1}})
}

// joinAndAnnounce makes join idempotent and, only on a genuinely new
// membership, drops a system message so existing members see who arrived.
func (s *Server) joinAndAnnounce(ctx context.Context, g store.ChatGroup, u store.ChatUser) {
	added, err := s.store.JoinGroup(ctx, g.ID, u.ID)
	if err != nil || !added {
		return
	}
	s.hub.Broadcast(g.ID, chat.Envelope{Type: chat.TypeJoin, UserID: u.ID, Name: u.Name})
	m := store.ChatMessage{
		GroupID: g.ID, UserID: u.ID, Kind: "system",
		Body: u.Name + " joined", CreatedAt: time.Now(),
	}
	if id, err := s.store.InsertChatMessage(ctx, m); err == nil {
		m.ID = id
		m.UserName = u.Name
		s.broadcastChatMessage(m)
	}
}

func (s *Server) broadcastChatMessage(m store.ChatMessage) {
	s.hub.Broadcast(m.GroupID, chat.Envelope{
		Type: chat.TypeMessage, ID: m.ID, UserID: m.UserID, Name: m.UserName,
		Kind: m.Kind, Body: m.Body,
		CreatedAt: m.CreatedAt.UTC().Format(time.RFC3339),
	})
}

// requireMembership resolves the {id} group and rejects non-members. Returns
// ok=false after writing the error response.
func (s *Server) requireMembership(w http.ResponseWriter, r *http.Request) (groupID string, u store.ChatUser, ok bool) {
	u = chatUserFrom(r)
	groupID = chi.URLParam(r, "id")
	member, err := s.store.IsMember(r.Context(), groupID, u.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "membership lookup failed")
		return "", u, false
	}
	if !member {
		writeErr(w, http.StatusForbidden, "not a member of this group")
		return "", u, false
	}
	return groupID, u, true
}

func (s *Server) handleChatMessages(w http.ResponseWriter, r *http.Request) {
	groupID, _, ok := s.requireMembership(w, r)
	if !ok {
		return
	}
	before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	msgs, err := s.store.ListChatMessages(r.Context(), groupID, before, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not load messages")
		return
	}
	out := make([]chatMessageJSON, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, toMessageJSON(m))
	}
	writeJSON(w, http.StatusOK, envelope{Data: out, Meta: meta{Total: len(out), Limit: limit}})
}

// handleChatSendMessage is the HTTP twin of the WS "message" frame: it uses
// the same persist-then-broadcast path, which keeps chat testable with plain
// curl and gives clients an offline-tolerant fallback.
func (s *Server) handleChatSendMessage(w http.ResponseWriter, r *http.Request) {
	groupID, u, ok := s.requireMembership(w, r)
	if !ok {
		return
	}
	var req struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	body := strings.TrimSpace(req.Body)
	if body == "" || utf8.RuneCountInString(body) > 2000 {
		writeErr(w, http.StatusBadRequest, "body must be 1-2000 characters")
		return
	}
	m := store.ChatMessage{
		GroupID: groupID, UserID: u.ID, UserName: u.Name,
		Kind: "text", Body: body, CreatedAt: time.Now(),
	}
	id, err := s.store.InsertChatMessage(r.Context(), m)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not store message")
		return
	}
	m.ID = id
	s.broadcastChatMessage(m)
	writeJSON(w, http.StatusCreated, envelope{Data: toMessageJSON(m), Meta: meta{Total: 1}})
}

// --- WebSocket ---

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// The token is the auth; mobile clients send no Origin header at all, so
	// an origin allowlist here would only lock out the app.
	CheckOrigin: func(*http.Request) bool { return true },
}

// handleChatWS authenticates via ?token= (WebSocket clients can't reliably
// set headers), upgrades, and hands the socket to the hub, pre-subscribed to
// all of the user's groups.
func (s *Server) handleChatWS(w http.ResponseWriter, r *http.Request) {
	tok := r.URL.Query().Get("token")
	if tok == "" {
		writeErr(w, http.StatusUnauthorized, "missing token")
		return
	}
	u, ok, err := s.store.GetChatUserByToken(r.Context(), tok)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "auth lookup failed")
		return
	}
	if !ok {
		writeErr(w, http.StatusUnauthorized, "invalid token")
		return
	}
	groups, err := s.store.ListGroupsForUser(r.Context(), u.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not load groups")
		return
	}
	ids := make([]string, 0, len(groups))
	for _, g := range groups {
		ids = append(ids, g.ID)
	}
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return // Upgrade already wrote the error
	}
	s.hub.HandleConn(conn, u.ID, u.Name, ids) // blocks for the connection's lifetime
}
