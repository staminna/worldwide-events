// Package chat implements the realtime layer for group chat and live
// location sharing: a WebSocket hub keyed by group, with persisted text
// messages (via an injected callback — this package never touches the store)
// and ephemeral in-memory location shares.
package chat

// Envelope is the single JSON wire format, both directions, discriminated by
// Type. Unused fields are omitted per message kind.
//
// client → server: message, location, location_stop, sub
// server → client: message, location, location_stop, presence, join, leave, error
type Envelope struct {
	Type    string `json:"type"`
	GroupID string `json:"groupId,omitempty"`

	// message
	ID        int64  `json:"id,omitempty"`
	UserID    string `json:"userId,omitempty"`
	Name      string `json:"name,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Body      string `json:"body,omitempty"`
	ClientRef string `json:"clientRef,omitempty"`
	CreatedAt string `json:"createdAt,omitempty"` // RFC3339

	// location
	Lat float64 `json:"lat,omitempty"`
	Lon float64 `json:"lon,omitempty"`
	Acc float64 `json:"acc,omitempty"`
	At  string  `json:"at,omitempty"` // RFC3339

	// presence
	Online  []string `json:"online,omitempty"`
	Sharing []Share  `json:"sharing,omitempty"`

	// error
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// Share is one active location share inside a presence snapshot.
type Share struct {
	UserID string  `json:"userId"`
	Name   string  `json:"name"`
	Lat    float64 `json:"lat"`
	Lon    float64 `json:"lon"`
	Acc    float64 `json:"acc,omitempty"`
	At     string  `json:"at"` // RFC3339
}

const (
	TypeMessage      = "message"
	TypeLocation     = "location"
	TypeLocationStop = "location_stop"
	TypeSub          = "sub"
	TypePresence     = "presence"
	TypeJoin         = "join"
	TypeLeave        = "leave"
	TypeError        = "error"
)
