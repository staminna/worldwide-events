package store

import (
	"context"
	"time"

	"github.com/jorgenunes/eventscraper/internal/model"
)

type Query struct {
	// CityID matches the catalog city an event was scraped for (exact,
	// e.g. "lisbon"). This is what the API and CLI filter on.
	CityID string
	// City matches the stored display city — the venue's own locality
	// string (e.g. "Carnaxide"). Used by the MCP server for free-text
	// city input that doesn't resolve to a catalog entry.
	City     string
	Category model.Category
	Source   model.Source
	From     time.Time
	To       time.Time
	// NotEndedBefore, when non-zero, hides events that already finished by
	// that instant: an event is kept while its ends_at (or, lacking one,
	// starts_at plus a grace window) is still in the future.
	NotEndedBefore time.Time
	Search         string
	Limit          int
	Offset         int
	RequireImage   bool
	// RequireCoords keeps only events whose venue has coordinates (used by
	// the GeoJSON export, where unlocated events are useless).
	RequireCoords bool
}

type ScrapeStatus struct {
	Source     model.Source
	CityID     string
	LastRunAt  time.Time
	ExpiresAt  time.Time
	Status     string
	ErrMessage string
}

// ChatUser is an anonymous chat identity: a server-issued id plus an opaque
// bearer token. There are no passwords — possession of the token is the
// identity.
type ChatUser struct {
	ID        string
	Name      string
	Token     string
	CreatedAt time.Time
}

// ChatGroup is a chat room: either the public room of an event
// (Type "event", EventID set) or a private invite-code group
// (Type "private", InviteCode set).
type ChatGroup struct {
	ID          string
	Type        string // "event" | "private"
	EventID     string // set when Type == "event"
	Name        string
	InviteCode  string // set when Type == "private"
	CreatedBy   string
	CreatedAt   time.Time
	MemberCount int       // filled by ListGroupsForUser
	LastMsgBody string    // filled by ListGroupsForUser; empty if no messages
	LastMsgAt   time.Time // zero if no messages
}

// ChatUserAdmin is the ops view of a chat user: identity plus activity
// counts. The token stays server-side — admin responses never serialize it.
type ChatUserAdmin struct {
	ChatUser
	GroupCount   int
	MessageCount int
}

// ChatMessage is a persisted group message. Location fixes are never stored;
// only "text" and "system" kinds reach this table.
type ChatMessage struct {
	ID        int64
	GroupID   string
	UserID    string
	UserName  string // joined from chat_users on read; not stored
	Kind      string // "text" | "system"
	Body      string
	CreatedAt time.Time
}

type Store interface {
	Init(ctx context.Context) error
	UpsertEvents(ctx context.Context, events []model.Event) error
	GetEvent(ctx context.Context, id string) (model.Event, bool, error)
	Query(ctx context.Context, q Query) ([]model.Event, int, time.Time, error)
	MarkScrape(ctx context.Context, s ScrapeStatus) error
	GetScrape(ctx context.Context, src model.Source, cityID string) (ScrapeStatus, bool, error)
	AllScrapes(ctx context.Context) ([]ScrapeStatus, error)
	// ClearImageURLsMatching strips imageUrl from any stored payload whose
	// JSON imageUrl field matches the given LIKE-style patterns. Returns the
	// number of rows updated.
	ClearImageURLsMatching(ctx context.Context, patterns []string) (int, error)
	// CountLocatedUpcoming counts a city's events that have venue
	// coordinates and haven't ended by the given instant.
	CountLocatedUpcoming(ctx context.Context, cityID string, notEndedBefore time.Time) (int, error)
	// GetGeoAddress / PutGeoAddress cache reverse-geocoded street addresses
	// by rounded-coordinate key. An empty stored address is a negative-cache
	// entry ("looked up, nothing there"); resolvedAt lets callers decide
	// when a negative is stale enough to retry.
	GetGeoAddress(ctx context.Context, key string) (addr string, resolvedAt time.Time, found bool, err error)
	PutGeoAddress(ctx context.Context, key, addr string) error
	// SetVenueAddressIfEmpty patches the stored event's venue.address only
	// when it is currently empty. Returns whether a row was changed.
	SetVenueAddressIfEmpty(ctx context.Context, eventID, addr string) (bool, error)

	// --- chat ---
	CreateChatUser(ctx context.Context, u ChatUser) error
	GetChatUserByToken(ctx context.Context, token string) (ChatUser, bool, error)
	CreateGroup(ctx context.Context, g ChatGroup) error
	GetGroup(ctx context.Context, id string) (ChatGroup, bool, error)
	GetGroupByInvite(ctx context.Context, code string) (ChatGroup, bool, error)
	// GetOrCreateEventGroup returns the event's room, creating it when it
	// doesn't exist yet. Safe under concurrent callers (insert-or-ignore on
	// the unique event_id index, then read back).
	GetOrCreateEventGroup(ctx context.Context, g ChatGroup) (ChatGroup, error)
	// JoinGroup adds the user to the group; idempotent. Reports whether a
	// membership row was actually created (false = was already a member).
	JoinGroup(ctx context.Context, groupID, userID string) (bool, error)
	LeaveGroup(ctx context.Context, groupID, userID string) error
	IsMember(ctx context.Context, groupID, userID string) (bool, error)
	ListGroupsForUser(ctx context.Context, userID string) ([]ChatGroup, error)
	InsertChatMessage(ctx context.Context, m ChatMessage) (int64, error)
	// ListChatMessages returns up to limit messages of a group, newest
	// first. beforeID = 0 means "from the latest"; otherwise only messages
	// with id < beforeID are returned (pagination cursor).
	ListChatMessages(ctx context.Context, groupID string, beforeID int64, limit int) ([]ChatMessage, error)

	// --- chat admin (ops surface, gated by ADMIN_TOKEN at the API layer) ---
	ListChatUsers(ctx context.Context) ([]ChatUserAdmin, error)
	ListAllGroups(ctx context.Context) ([]ChatGroup, error)
	// DeleteChatUser removes the user (revoking its token) and its
	// memberships. Messages are kept; their author renders as "?".
	DeleteChatUser(ctx context.Context, id string) error
	// DeleteChatGroup removes the group, its memberships, and its messages.
	DeleteChatGroup(ctx context.Context, id string) error

	Close() error
}
