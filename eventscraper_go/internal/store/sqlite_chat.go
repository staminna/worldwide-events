package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const chatSchemaSQLite = `
CREATE TABLE IF NOT EXISTS chat_users (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    token      TEXT NOT NULL UNIQUE,
    created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS chat_groups (
    id          TEXT PRIMARY KEY,
    type        TEXT NOT NULL,
    event_id    TEXT,
    name        TEXT NOT NULL,
    invite_code TEXT UNIQUE,
    created_by  TEXT NOT NULL DEFAULT '',
    created_at  INTEGER NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_chat_groups_event
    ON chat_groups(event_id) WHERE event_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS chat_members (
    group_id  TEXT NOT NULL,
    user_id   TEXT NOT NULL,
    joined_at INTEGER NOT NULL,
    PRIMARY KEY (group_id, user_id)
);
CREATE INDEX IF NOT EXISTS idx_chat_members_user ON chat_members(user_id);

CREATE TABLE IF NOT EXISTS chat_messages (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    group_id   TEXT NOT NULL,
    user_id    TEXT NOT NULL,
    kind       TEXT NOT NULL DEFAULT 'text',
    body       TEXT NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_chat_messages_group ON chat_messages(group_id, id);
`

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (s *SQLite) CreateChatUser(ctx context.Context, u ChatUser) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO chat_users (id, name, token, created_at) VALUES (?,?,?,?)`,
		u.ID, u.Name, u.Token, u.CreatedAt.Unix())
	return err
}

func (s *SQLite) GetChatUserByToken(ctx context.Context, token string) (ChatUser, bool, error) {
	var u ChatUser
	var createdAt int64
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, token, created_at FROM chat_users WHERE token = ?`, token,
	).Scan(&u.ID, &u.Name, &u.Token, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ChatUser{}, false, nil
	}
	if err != nil {
		return ChatUser{}, false, err
	}
	u.CreatedAt = time.Unix(createdAt, 0)
	return u, true, nil
}

func (s *SQLite) CreateGroup(ctx context.Context, g ChatGroup) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO chat_groups (id, type, event_id, name, invite_code, created_by, created_at)
		 VALUES (?,?,?,?,?,?,?)`,
		g.ID, g.Type, nullIfEmpty(g.EventID), g.Name, nullIfEmpty(g.InviteCode), g.CreatedBy, g.CreatedAt.Unix())
	return err
}

const sqliteGroupCols = `id, type, COALESCE(event_id,''), name, COALESCE(invite_code,''), created_by, created_at`

func scanSQLiteGroup(row *sql.Row) (ChatGroup, bool, error) {
	var g ChatGroup
	var createdAt int64
	err := row.Scan(&g.ID, &g.Type, &g.EventID, &g.Name, &g.InviteCode, &g.CreatedBy, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ChatGroup{}, false, nil
	}
	if err != nil {
		return ChatGroup{}, false, err
	}
	g.CreatedAt = time.Unix(createdAt, 0)
	return g, true, nil
}

func (s *SQLite) GetGroup(ctx context.Context, id string) (ChatGroup, bool, error) {
	return scanSQLiteGroup(s.db.QueryRowContext(ctx,
		`SELECT `+sqliteGroupCols+` FROM chat_groups WHERE id = ?`, id))
}

func (s *SQLite) GetGroupByInvite(ctx context.Context, code string) (ChatGroup, bool, error) {
	return scanSQLiteGroup(s.db.QueryRowContext(ctx,
		`SELECT `+sqliteGroupCols+` FROM chat_groups WHERE invite_code = ?`, code))
}

func (s *SQLite) GetOrCreateEventGroup(ctx context.Context, g ChatGroup) (ChatGroup, error) {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO chat_groups (id, type, event_id, name, invite_code, created_by, created_at)
		 VALUES (?,?,?,?,NULL,?,?)`,
		g.ID, g.Type, g.EventID, g.Name, g.CreatedBy, g.CreatedAt.Unix())
	if err != nil {
		return ChatGroup{}, err
	}
	got, ok, err := scanSQLiteGroup(s.db.QueryRowContext(ctx,
		`SELECT `+sqliteGroupCols+` FROM chat_groups WHERE event_id = ?`, g.EventID))
	if err != nil {
		return ChatGroup{}, err
	}
	if !ok {
		return ChatGroup{}, errors.New("event group vanished after insert")
	}
	return got, nil
}

func (s *SQLite) JoinGroup(ctx context.Context, groupID, userID string) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO chat_members (group_id, user_id, joined_at) VALUES (?,?,?)`,
		groupID, userID, time.Now().Unix())
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *SQLite) LeaveGroup(ctx context.Context, groupID, userID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM chat_members WHERE group_id = ? AND user_id = ?`, groupID, userID)
	return err
}

func (s *SQLite) IsMember(ctx context.Context, groupID, userID string) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx,
		`SELECT 1 FROM chat_members WHERE group_id = ? AND user_id = ?`, groupID, userID,
	).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (s *SQLite) ListGroupsForUser(ctx context.Context, userID string) ([]ChatGroup, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT g.id, g.type, COALESCE(g.event_id,''), g.name, COALESCE(g.invite_code,''),
		       g.created_by, g.created_at,
		       (SELECT COUNT(*) FROM chat_members mc WHERE mc.group_id = g.id),
		       COALESCE((SELECT body FROM chat_messages lm WHERE lm.group_id = g.id ORDER BY lm.id DESC LIMIT 1), ''),
		       COALESCE((SELECT created_at FROM chat_messages lm WHERE lm.group_id = g.id ORDER BY lm.id DESC LIMIT 1), 0)
		FROM chat_groups g
		JOIN chat_members m ON m.group_id = g.id
		WHERE m.user_id = ?
		ORDER BY MAX(g.created_at, COALESCE((SELECT created_at FROM chat_messages lm WHERE lm.group_id = g.id ORDER BY lm.id DESC LIMIT 1), 0)) DESC`,
		userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChatGroup
	for rows.Next() {
		var g ChatGroup
		var createdAt, lastMsgAt int64
		if err := rows.Scan(&g.ID, &g.Type, &g.EventID, &g.Name, &g.InviteCode,
			&g.CreatedBy, &createdAt, &g.MemberCount, &g.LastMsgBody, &lastMsgAt); err != nil {
			return nil, err
		}
		g.CreatedAt = time.Unix(createdAt, 0)
		if lastMsgAt > 0 {
			g.LastMsgAt = time.Unix(lastMsgAt, 0)
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *SQLite) InsertChatMessage(ctx context.Context, m ChatMessage) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO chat_messages (group_id, user_id, kind, body, created_at)
		 VALUES (?,?,?,?,?) RETURNING id`,
		m.GroupID, m.UserID, m.Kind, m.Body, m.CreatedAt.Unix()).Scan(&id)
	return id, err
}

func (s *SQLite) ListChatMessages(ctx context.Context, groupID string, beforeID int64, limit int) ([]ChatMessage, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.id, m.group_id, m.user_id, COALESCE(u.name, '?'), m.kind, m.body, m.created_at
		FROM chat_messages m
		LEFT JOIN chat_users u ON u.id = m.user_id
		WHERE m.group_id = ? AND (? = 0 OR m.id < ?)
		ORDER BY m.id DESC
		LIMIT ?`,
		groupID, beforeID, beforeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanChatMessages(rows)
}

// scanChatMessages consumes a rows cursor produced by the shared message
// SELECT column order (works for both database/sql via this file and any
// identical projection).
func scanChatMessages(rows *sql.Rows) ([]ChatMessage, error) {
	var out []ChatMessage
	for rows.Next() {
		var m ChatMessage
		var createdAt int64
		if err := rows.Scan(&m.ID, &m.GroupID, &m.UserID, &m.UserName, &m.Kind, &m.Body, &createdAt); err != nil {
			return nil, err
		}
		m.CreatedAt = time.Unix(createdAt, 0)
		out = append(out, m)
	}
	return out, rows.Err()
}
