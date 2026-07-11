package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/jorgenunes/eventscraper/internal/chat"
	"github.com/jorgenunes/eventscraper/internal/config"
)

// chatTestServer boots the full router (real SQLite store, real hub) so the
// test exercises the same wire path as the app: REST + WebSocket.
func chatTestServer(t *testing.T) (*httptest.Server, *Server) {
	t.Helper()
	st := newTestStore(t)
	s := NewServer(config.Config{AllowedOrigin: "*"}, st, nil, nil, nil)
	ts := httptest.NewServer(s.Router())
	t.Cleanup(ts.Close)
	return ts, s
}

func chatPost(t *testing.T, ts *httptest.Server, path, token, body string) map[string]any {
	t.Helper()
	req, _ := http.NewRequest("POST", ts.URL+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		t.Fatalf("POST %s: status %d", path, resp.StatusCode)
	}
	var out struct {
		Data map[string]any `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("POST %s: decode: %v", path, err)
	}
	return out.Data
}

func register(t *testing.T, ts *httptest.Server, name string) (id, token string) {
	t.Helper()
	data := chatPost(t, ts, "/chat/register", "", `{"name":"`+name+`"}`)
	return data["id"].(string), data["token"].(string)
}

func dialWS(t *testing.T, ts *httptest.Server, token string) *websocket.Conn {
	t.Helper()
	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/chat/ws?token=" + token
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// readEnvelope reads frames until one matches wantType (skipping unrelated
// broadcasts like join/presence), failing the test after a deadline.
func readEnvelope(t *testing.T, conn *websocket.Conn, wantType string) chat.Envelope {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		var env chat.Envelope
		if err := conn.ReadJSON(&env); err != nil {
			t.Fatalf("waiting for %q frame: %v", wantType, err)
		}
		if env.Type == wantType {
			return env
		}
	}
}

func TestChatEndToEnd(t *testing.T) {
	ts, _ := chatTestServer(t)

	// Identity + private group + invite join over REST.
	jorgeID, jorgeTok := register(t, ts, "Jorge")
	_, anaTok := register(t, ts, "Ana")

	group := chatPost(t, ts, "/chat/groups", jorgeTok, `{"name":"night crew"}`)
	gid := group["id"].(string)
	code := group["inviteCode"].(string)
	if group["type"] != "private" || len(code) != 6 {
		t.Fatalf("created group = %+v", group)
	}
	joined := chatPost(t, ts, "/chat/groups/join", anaTok, `{"code":"`+code+`"}`)
	if joined["id"] != gid {
		t.Fatalf("join by code returned %v, want %s", joined["id"], gid)
	}

	// Both connect. Jorge sends over WS; Ana must receive it live, and the
	// echo back to Jorge must carry his clientRef for reconciliation.
	jorgeWS := dialWS(t, ts, jorgeTok)
	anaWS := dialWS(t, ts, anaTok)
	if err := jorgeWS.WriteJSON(chat.Envelope{Type: "message", GroupID: gid, Body: "on my way", ClientRef: "c-1"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := readEnvelope(t, anaWS, "message")
	if got.Body != "on my way" || got.UserID != jorgeID || got.Name != "Jorge" {
		t.Fatalf("ana received %+v", got)
	}
	echo := readEnvelope(t, jorgeWS, "message")
	if echo.ClientRef != "c-1" {
		t.Fatalf("sender echo clientRef = %q, want c-1", echo.ClientRef)
	}

	// REST send fans out to live sockets too (shared persist+broadcast path).
	chatPost(t, ts, "/chat/groups/"+gid+"/messages", anaTok, `{"body":"see you there"}`)
	if got := readEnvelope(t, jorgeWS, "message"); got.Body != "see you there" {
		t.Fatalf("jorge received %+v", got)
	}

	// History pages newest-first and includes the system join messages.
	req, _ := http.NewRequest("GET", ts.URL+"/chat/groups/"+gid+"/messages?limit=50", nil)
	req.Header.Set("Authorization", "Bearer "+anaTok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET messages: %v", err)
	}
	defer resp.Body.Close()
	var hist struct {
		Data []struct {
			Body string `json:"body"`
			Kind string `json:"kind"`
		} `json:"data"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&hist)
	if len(hist.Data) != 4 { // 2 system joins + 2 texts
		t.Fatalf("history len = %d, want 4: %+v", len(hist.Data), hist.Data)
	}
	if hist.Data[0].Body != "see you there" || hist.Data[3].Kind != "system" {
		t.Fatalf("history order wrong: %+v", hist.Data)
	}

	// Location: Jorge shares, Ana sees the dot move; a late joiner gets the
	// last fix in the presence snapshot; stop fans out.
	if err := jorgeWS.WriteJSON(chat.Envelope{Type: "location", GroupID: gid, Lat: 38.7223, Lon: -9.1393, Acc: 10}); err != nil {
		t.Fatalf("write location: %v", err)
	}
	loc := readEnvelope(t, anaWS, "location")
	if loc.UserID != jorgeID || loc.Lat != 38.7223 {
		t.Fatalf("ana location frame = %+v", loc)
	}

	_, ruiTok := register(t, ts, "Rui")
	chatPost(t, ts, "/chat/groups/join", ruiTok, `{"code":"`+code+`"}`)
	ruiWS := dialWS(t, ts, ruiTok)
	pres := readEnvelope(t, ruiWS, "presence")
	if len(pres.Sharing) != 1 || pres.Sharing[0].UserID != jorgeID {
		t.Fatalf("late-joiner presence = %+v", pres)
	}

	if err := jorgeWS.WriteJSON(chat.Envelope{Type: "location_stop", GroupID: gid}); err != nil {
		t.Fatalf("write stop: %v", err)
	}
	stop := readEnvelope(t, anaWS, "location_stop")
	if stop.UserID != jorgeID {
		t.Fatalf("stop frame = %+v", stop)
	}

	// Non-members are rejected on both paths.
	_, mallTok := register(t, ts, "Mallory")
	req, _ = http.NewRequest("POST", ts.URL+"/chat/groups/"+gid+"/messages", strings.NewReader(`{"body":"hi"}`))
	req.Header.Set("Authorization", "Bearer "+mallTok)
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("mallory POST: %v", err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusForbidden {
		t.Fatalf("non-member REST send status = %d, want 403", resp2.StatusCode)
	}
	mallWS := dialWS(t, ts, mallTok)
	_ = mallWS.WriteJSON(chat.Envelope{Type: "message", GroupID: gid, Body: "sneak"})
	if errEnv := readEnvelope(t, mallWS, "error"); errEnv.Code != "not_member" {
		t.Fatalf("non-member WS send error = %+v", errEnv)
	}
}

func TestChatEventRoom(t *testing.T) {
	ts, srv := chatTestServer(t)
	ev := locatedEvent(t, srv.store, "jazz-1", "lisbon", 38.72, -9.13)

	_, tok1 := register(t, ts, "Jorge")
	_, tok2 := register(t, ts, "Ana")

	room1 := chatPost(t, ts, "/chat/events/"+ev.ID+"/join", tok1, `{}`)
	room2 := chatPost(t, ts, "/chat/events/"+ev.ID+"/join", tok2, `{}`)
	if room1["id"] != room2["id"] {
		t.Fatalf("event room not shared: %v vs %v", room1["id"], room2["id"])
	}
	if room1["type"] != "event" || room1["name"] != ev.Title {
		t.Fatalf("event room = %+v", room1)
	}
	// Idempotent re-join keeps a single membership.
	again := chatPost(t, ts, "/chat/events/"+ev.ID+"/join", tok1, `{}`)
	if again["id"] != room1["id"] {
		t.Fatalf("re-join changed room: %v", again["id"])
	}

	// Unknown event 404s.
	req, _ := http.NewRequest("POST", ts.URL+"/chat/events/nope/join", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+tok1)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown event status = %d, want 404", resp.StatusCode)
	}
}
