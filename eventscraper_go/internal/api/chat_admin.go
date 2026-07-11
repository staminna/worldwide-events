package api

import (
	_ "embed"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// The chat admin console is a self-contained page (no CDN) that calls the
// admin JSON endpoints below with an Authorization header via fetch (relative
// URLs, so it works behind a reverse-proxy path prefix). The page itself
// holds no data, so serving it is public — the data and the delete actions
// are what requireAdmin gates.
//
//go:embed static/chatadmin.html
var chatAdminHTML []byte

func (s *Server) handleChatAdmin(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(chatAdminHTML)
}

type chatUserAdminJSON struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	CreatedAt    string `json:"createdAt"`
	GroupCount   int    `json:"groupCount"`
	MessageCount int    `json:"messageCount"`
}

// handleChatAdminData returns everything the console renders in one call:
// all users (with activity counts, never tokens) and all groups.
func (s *Server) handleChatAdminData(w http.ResponseWriter, r *http.Request) {
	users, err := s.store.ListChatUsers(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not list users")
		return
	}
	groups, err := s.store.ListAllGroups(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not list groups")
		return
	}
	userOut := make([]chatUserAdminJSON, 0, len(users))
	for _, u := range users {
		userOut = append(userOut, chatUserAdminJSON{
			ID: u.ID, Name: u.Name,
			CreatedAt:    u.CreatedAt.UTC().Format(time.RFC3339),
			GroupCount:   u.GroupCount,
			MessageCount: u.MessageCount,
		})
	}
	groupOut := make([]chatGroupJSON, 0, len(groups))
	for _, g := range groups {
		groupOut = append(groupOut, toGroupJSON(g))
	}
	writeJSON(w, http.StatusOK, envelope{
		Data: map[string]any{"users": userOut, "groups": groupOut},
		Meta: meta{Total: len(userOut) + len(groupOut)},
	})
}

func (s *Server) handleChatAdminDeleteUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.store.DeleteChatUser(r.Context(), id); err != nil {
		writeErr(w, http.StatusInternalServerError, "could not delete user")
		return
	}
	writeJSON(w, http.StatusOK, envelope{Data: map[string]bool{"deleted": true}, Meta: meta{Total: 1}})
}

func (s *Server) handleChatAdminDeleteGroup(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.store.DeleteChatGroup(r.Context(), id); err != nil {
		writeErr(w, http.StatusInternalServerError, "could not delete group")
		return
	}
	writeJSON(w, http.StatusOK, envelope{Data: map[string]bool{"deleted": true}, Meta: meta{Total: 1}})
}
