package chat

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Tunables. Deliberately constants, not config — none of these need to vary
// per deployment (see plan: simplest working solution first).
const (
	// shareStaleAfter sweeps a location share whose last fix is older than
	// this. Clients heartbeat every ~20s even when stationary, so 2 minutes
	// means "the sharer silently vanished".
	shareStaleAfter = 2 * time.Minute
	// shareHardCap ends any share session regardless of heartbeats, so a
	// forgotten toggle can't broadcast someone's location all week.
	shareHardCap = 3 * time.Hour
	janitorEvery = 30 * time.Second

	pingInterval = 30 * time.Second // < nginx's default 60s proxy_read_timeout
	pongWait     = 60 * time.Second
	writeWait    = 10 * time.Second
	maxFrameSize = 8 << 10
	sendBuffer   = 64

	// Per-connection chat-message rate limit (token bucket).
	msgRefillPerSec = 1.0
	msgBurst        = 5
	// Location fixes faster than this are silently dropped; well-behaved
	// clients throttle to >=5s themselves.
	minFixInterval = 2 * time.Second
)

// PersistFunc stores a text message and returns its id and timestamp.
type PersistFunc func(ctx context.Context, groupID, userID, body string) (id int64, createdAt time.Time, err error)

// MembershipFunc reports whether the user belongs to the group.
type MembershipFunc func(ctx context.Context, groupID, userID string) (bool, error)

type fix struct {
	name      string
	lat, lon  float64
	acc       float64
	at        time.Time
	startedAt time.Time
}

// Hub is the mutex-guarded fan-out core: which client sockets are subscribed
// to which group, plus the ephemeral last-known location share per
// (group, user). Same style as scheduler.RunTracker — plain mutex methods,
// state is lost on restart by design (clients re-subscribe and re-send their
// fix on reconnect).
type Hub struct {
	persist  PersistFunc
	isMember MembershipFunc

	mu     sync.Mutex
	rooms  map[string]map[*Client]struct{} // groupID -> subscribed clients
	shares map[string]map[string]fix       // groupID -> userID -> last fix
}

func NewHub(persist PersistFunc, isMember MembershipFunc) *Hub {
	h := &Hub{
		persist:  persist,
		isMember: isMember,
		rooms:    make(map[string]map[*Client]struct{}),
		shares:   make(map[string]map[string]fix),
	}
	go h.janitor()
	return h
}

// HandleConn takes ownership of an upgraded connection: subscribes it to all
// of the user's groups, sends presence snapshots, and runs the read/write
// pumps. Blocks until the connection dies (callers run it on the request
// goroutine — chi handlers are one goroutine per connection anyway).
func (h *Hub) HandleConn(conn *websocket.Conn, userID, name string, groupIDs []string) {
	c := &Client{
		hub:     h,
		conn:    conn,
		userID:  userID,
		name:    name,
		send:    make(chan []byte, sendBuffer),
		groups:  make(map[string]struct{}, len(groupIDs)),
		sharing: make(map[string]struct{}),
	}
	h.mu.Lock()
	for _, gid := range groupIDs {
		c.groups[gid] = struct{}{}
		h.subscribeLocked(c, gid)
	}
	h.mu.Unlock()

	// Late joiners see active sharers immediately: presence snapshot per
	// group that has any.
	for _, gid := range groupIDs {
		if env, ok := h.presence(gid); ok {
			c.enqueue(mustJSON(env))
		}
	}

	go c.writePump()
	c.readPump() // blocks; cleans up via h.dropClient on exit
}

func (h *Hub) subscribeLocked(c *Client, groupID string) {
	room := h.rooms[groupID]
	if room == nil {
		room = make(map[*Client]struct{})
		h.rooms[groupID] = room
	}
	room[c] = struct{}{}
}

// Subscribe adds the client to a group's fan-out (after the caller verified
// membership) and returns the presence snapshot to send back.
func (h *Hub) Subscribe(c *Client, groupID string) Envelope {
	h.mu.Lock()
	h.subscribeLocked(c, groupID)
	h.mu.Unlock()
	env, _ := h.presence(groupID)
	return env
}

// dropClient removes the client from every room and ends any location shares
// it had running (foreground-only v1: a dropped socket means the sharer is
// gone, so peers should stop seeing the dot).
func (h *Hub) dropClient(c *Client) {
	h.mu.Lock()
	for gid := range c.groups {
		if room := h.rooms[gid]; room != nil {
			delete(room, c)
			if len(room) == 0 {
				delete(h.rooms, gid)
			}
		}
	}
	stopped := make([]string, 0, len(c.sharing))
	for gid := range c.sharing {
		if byUser := h.shares[gid]; byUser != nil {
			delete(byUser, c.userID)
			if len(byUser) == 0 {
				delete(h.shares, gid)
			}
		}
		stopped = append(stopped, gid)
	}
	h.mu.Unlock()

	for _, gid := range stopped {
		h.Broadcast(gid, Envelope{Type: TypeLocationStop, GroupID: gid, UserID: c.userID})
	}
	c.closeSend()
}

// Broadcast fans an envelope out to every socket subscribed to the group.
// Marshals once; slow consumers are dropped rather than blocking the hub.
func (h *Hub) Broadcast(groupID string, env Envelope) {
	env.GroupID = groupID
	payload := mustJSON(env)

	h.mu.Lock()
	targets := make([]*Client, 0, len(h.rooms[groupID]))
	for c := range h.rooms[groupID] {
		targets = append(targets, c)
	}
	h.mu.Unlock()

	for _, c := range targets {
		c.enqueue(payload)
	}
}

// SetShare records a fix and broadcasts it. Returns false when the fix was
// dropped (too frequent or the session exceeded the hard cap).
func (h *Hub) SetShare(groupID, userID, name string, lat, lon, acc float64) bool {
	now := time.Now()
	h.mu.Lock()
	byUser := h.shares[groupID]
	if byUser == nil {
		byUser = make(map[string]fix)
		h.shares[groupID] = byUser
	}
	prev, existed := byUser[userID]
	if existed && now.Sub(prev.at) < minFixInterval {
		h.mu.Unlock()
		return false
	}
	started := now
	if existed {
		started = prev.startedAt
		if now.Sub(started) > shareHardCap {
			delete(byUser, userID)
			h.mu.Unlock()
			h.Broadcast(groupID, Envelope{Type: TypeLocationStop, GroupID: groupID, UserID: userID})
			return false
		}
	}
	byUser[userID] = fix{name: name, lat: lat, lon: lon, acc: acc, at: now, startedAt: started}
	h.mu.Unlock()

	h.Broadcast(groupID, Envelope{
		Type: TypeLocation, GroupID: groupID, UserID: userID, Name: name,
		Lat: lat, Lon: lon, Acc: acc, At: now.UTC().Format(time.RFC3339),
	})
	return true
}

// StopShare ends a share session and tells the group.
func (h *Hub) StopShare(groupID, userID string) {
	h.mu.Lock()
	byUser := h.shares[groupID]
	_, existed := byUser[userID]
	if existed {
		delete(byUser, userID)
		if len(byUser) == 0 {
			delete(h.shares, groupID)
		}
	}
	h.mu.Unlock()
	if existed {
		h.Broadcast(groupID, Envelope{Type: TypeLocationStop, GroupID: groupID, UserID: userID})
	}
}

// presence builds the snapshot envelope for a group: who is online (has a
// subscribed socket) and every active sharer's last fix. ok is false when the
// group has neither.
func (h *Hub) presence(groupID string) (Envelope, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	online := make([]string, 0, len(h.rooms[groupID]))
	seen := map[string]bool{}
	for c := range h.rooms[groupID] {
		if !seen[c.userID] {
			seen[c.userID] = true
			online = append(online, c.userID)
		}
	}
	var sharing []Share
	for uid, f := range h.shares[groupID] {
		sharing = append(sharing, Share{
			UserID: uid, Name: f.name, Lat: f.lat, Lon: f.lon, Acc: f.acc,
			At: f.at.UTC().Format(time.RFC3339),
		})
	}
	if len(online) == 0 && len(sharing) == 0 {
		return Envelope{}, false
	}
	return Envelope{Type: TypePresence, GroupID: groupID, Online: online, Sharing: sharing}, true
}

// janitor sweeps stale and over-cap shares so a wedged client can't leave a
// ghost dot on everyone's map.
func (h *Hub) janitor() {
	for range time.Tick(janitorEvery) {
		type ended struct{ groupID, userID string }
		var stops []ended
		now := time.Now()
		h.mu.Lock()
		for gid, byUser := range h.shares {
			for uid, f := range byUser {
				if now.Sub(f.at) > shareStaleAfter || now.Sub(f.startedAt) > shareHardCap {
					delete(byUser, uid)
					stops = append(stops, ended{gid, uid})
				}
			}
			if len(byUser) == 0 {
				delete(h.shares, gid)
			}
		}
		h.mu.Unlock()
		for _, e := range stops {
			h.Broadcast(e.groupID, Envelope{Type: TypeLocationStop, GroupID: e.groupID, UserID: e.userID})
		}
	}
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		// Envelope contains only marshalable fields; this cannot happen.
		log.Printf("chat: marshal: %v", err)
		return []byte(`{"type":"error","code":"internal","message":"marshal failed"}`)
	}
	return b
}
