package chat

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Client is one WebSocket connection belonging to one chat user. A single
// socket multiplexes all of the user's groups. groups and sharing are only
// touched from the readPump goroutine; the hub's own maps are what Broadcast
// iterates.
type Client struct {
	hub    *Hub
	conn   *websocket.Conn
	userID string
	name   string

	send      chan []byte
	closeOnce sync.Once

	groups  map[string]struct{}
	sharing map[string]struct{}

	// message token bucket
	tokens   float64
	lastFill time.Time
}

// enqueue hands a frame to the write pump without ever blocking the hub. A
// consumer whose buffer is full is dead weight — closing its socket makes
// its pumps exit and the client reconnect fresh.
func (c *Client) enqueue(payload []byte) {
	select {
	case c.send <- payload:
	default:
		_ = c.conn.Close()
	}
}

func (c *Client) closeSend() {
	c.closeOnce.Do(func() { close(c.send) })
}

func (c *Client) allowMessage() bool {
	now := time.Now()
	if c.lastFill.IsZero() {
		c.tokens = msgBurst
	} else {
		c.tokens += now.Sub(c.lastFill).Seconds() * msgRefillPerSec
		if c.tokens > msgBurst {
			c.tokens = msgBurst
		}
	}
	c.lastFill = now
	if c.tokens < 1 {
		return false
	}
	c.tokens--
	return true
}

func (c *Client) sendError(code, msg string) {
	c.enqueue(mustJSON(Envelope{Type: TypeError, Code: code, Message: msg}))
}

// readPump owns inbound frames. Exits on any read error (disconnect, pong
// timeout, oversized frame) and tears the client down.
func (c *Client) readPump() {
	defer func() {
		c.hub.dropClient(c)
		_ = c.conn.Close()
	}()
	c.conn.SetReadLimit(maxFrameSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})
	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		var env Envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			c.sendError("bad_json", "frame is not valid JSON")
			continue
		}
		c.dispatch(env)
	}
}

func (c *Client) dispatch(env Envelope) {
	switch env.Type {
	case TypeMessage:
		if _, ok := c.groups[env.GroupID]; !ok {
			c.sendError("not_member", "you are not a member of this group")
			return
		}
		body := strings.TrimSpace(env.Body)
		if body == "" || len(body) > 2000 {
			c.sendError("bad_body", "message must be 1-2000 characters")
			return
		}
		if !c.allowMessage() {
			c.sendError("rate_limited", "slow down")
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		id, createdAt, err := c.hub.persist(ctx, env.GroupID, c.userID, body)
		cancel()
		if err != nil {
			c.sendError("persist_failed", "message could not be stored")
			return
		}
		c.hub.Broadcast(env.GroupID, Envelope{
			Type: TypeMessage, GroupID: env.GroupID, ID: id,
			UserID: c.userID, Name: c.name, Kind: "text", Body: body,
			CreatedAt: createdAt.UTC().Format(time.RFC3339), ClientRef: env.ClientRef,
		})

	case TypeLocation:
		if _, ok := c.groups[env.GroupID]; !ok {
			c.sendError("not_member", "you are not a member of this group")
			return
		}
		if env.Lat < -90 || env.Lat > 90 || env.Lon < -180 || env.Lon > 180 {
			c.sendError("bad_fix", "lat/lon out of range")
			return
		}
		if c.hub.SetShare(env.GroupID, c.userID, c.name, env.Lat, env.Lon, env.Acc) {
			c.sharing[env.GroupID] = struct{}{}
		}

	case TypeLocationStop:
		delete(c.sharing, env.GroupID)
		c.hub.StopShare(env.GroupID, c.userID)

	case TypeSub:
		// Sent after a REST join so the live socket picks the new group up
		// without reconnecting. Membership is re-verified against the DB —
		// the client's claim alone is not trusted.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		ok, err := c.hub.isMember(ctx, env.GroupID, c.userID)
		cancel()
		if err != nil || !ok {
			c.sendError("not_member", "you are not a member of this group")
			return
		}
		c.groups[env.GroupID] = struct{}{}
		presence := c.hub.Subscribe(c, env.GroupID)
		if presence.Type != "" {
			c.enqueue(mustJSON(presence))
		}

	default:
		c.sendError("unknown_type", "unsupported envelope type: "+env.Type)
	}
}

// writePump owns outbound frames and keepalive pings. Exits when send is
// closed (teardown) or a write fails.
func (c *Client) writePump() {
	ticker := time.NewTicker(pingInterval)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()
	for {
		select {
		case payload, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, payload); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
