package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

const chatSchemaPG = `
CREATE TABLE IF NOT EXISTS chat_users (
    id         text   PRIMARY KEY,
    name       text   NOT NULL,
    token      text   NOT NULL UNIQUE,
    created_at bigint NOT NULL
);

CREATE TABLE IF NOT EXISTS chat_groups (
    id          text   PRIMARY KEY,
    type        text   NOT NULL,
    event_id    text,
    name        text   NOT NULL,
    invite_code text   UNIQUE,
    created_by  text   NOT NULL DEFAULT '',
    created_at  bigint NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_chat_groups_event
    ON chat_groups(event_id) WHERE event_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS chat_members (
    group_id  text   NOT NULL,
    user_id   text   NOT NULL,
    joined_at bigint NOT NULL,
    PRIMARY KEY (group_id, user_id)
);
CREATE INDEX IF NOT EXISTS idx_chat_members_user ON chat_members(user_id);

CREATE TABLE IF NOT EXISTS chat_messages (
    id         bigserial PRIMARY KEY,
    group_id   text   NOT NULL,
    user_id    text   NOT NULL,
    kind       text   NOT NULL DEFAULT 'text',
    body       text   NOT NULL,
    created_at bigint NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_chat_messages_group ON chat_messages(group_id, id);
`

func (p *Postgres) CreateChatUser(ctx context.Context, u ChatUser) error {
	_, err := p.pool.Exec(ctx,
		`INSERT INTO chat_users (id, name, token, created_at) VALUES ($1,$2,$3,$4)`,
		u.ID, u.Name, u.Token, u.CreatedAt.Unix())
	return err
}

func (p *Postgres) GetChatUserByToken(ctx context.Context, token string) (ChatUser, bool, error) {
	var u ChatUser
	var createdAt int64
	err := p.pool.QueryRow(ctx,
		`SELECT id, name, token, created_at FROM chat_users WHERE token = $1`, token,
	).Scan(&u.ID, &u.Name, &u.Token, &createdAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ChatUser{}, false, nil
	}
	if err != nil {
		return ChatUser{}, false, err
	}
	u.CreatedAt = time.Unix(createdAt, 0)
	return u, true, nil
}

func (p *Postgres) CreateGroup(ctx context.Context, g ChatGroup) error {
	_, err := p.pool.Exec(ctx,
		`INSERT INTO chat_groups (id, type, event_id, name, invite_code, created_by, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		g.ID, g.Type, nullIfEmpty(g.EventID), g.Name, nullIfEmpty(g.InviteCode), g.CreatedBy, g.CreatedAt.Unix())
	return err
}

const pgGroupCols = `id, type, COALESCE(event_id,''), name, COALESCE(invite_code,''), created_by, created_at`

func scanPGGroup(row pgx.Row) (ChatGroup, bool, error) {
	var g ChatGroup
	var createdAt int64
	err := row.Scan(&g.ID, &g.Type, &g.EventID, &g.Name, &g.InviteCode, &g.CreatedBy, &createdAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ChatGroup{}, false, nil
	}
	if err != nil {
		return ChatGroup{}, false, err
	}
	g.CreatedAt = time.Unix(createdAt, 0)
	return g, true, nil
}

func (p *Postgres) GetGroup(ctx context.Context, id string) (ChatGroup, bool, error) {
	return scanPGGroup(p.pool.QueryRow(ctx,
		`SELECT `+pgGroupCols+` FROM chat_groups WHERE id = $1`, id))
}

func (p *Postgres) GetGroupByInvite(ctx context.Context, code string) (ChatGroup, bool, error) {
	return scanPGGroup(p.pool.QueryRow(ctx,
		`SELECT `+pgGroupCols+` FROM chat_groups WHERE invite_code = $1`, code))
}

func (p *Postgres) GetOrCreateEventGroup(ctx context.Context, g ChatGroup) (ChatGroup, error) {
	_, err := p.pool.Exec(ctx,
		`INSERT INTO chat_groups (id, type, event_id, name, invite_code, created_by, created_at)
		 VALUES ($1,$2,$3,$4,NULL,$5,$6)
		 ON CONFLICT DO NOTHING`,
		g.ID, g.Type, g.EventID, g.Name, g.CreatedBy, g.CreatedAt.Unix())
	if err != nil {
		return ChatGroup{}, err
	}
	got, ok, err := scanPGGroup(p.pool.QueryRow(ctx,
		`SELECT `+pgGroupCols+` FROM chat_groups WHERE event_id = $1`, g.EventID))
	if err != nil {
		return ChatGroup{}, err
	}
	if !ok {
		return ChatGroup{}, errors.New("event group vanished after insert")
	}
	return got, nil
}

func (p *Postgres) JoinGroup(ctx context.Context, groupID, userID string) (bool, error) {
	tag, err := p.pool.Exec(ctx,
		`INSERT INTO chat_members (group_id, user_id, joined_at) VALUES ($1,$2,$3)
		 ON CONFLICT DO NOTHING`,
		groupID, userID, time.Now().Unix())
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (p *Postgres) LeaveGroup(ctx context.Context, groupID, userID string) error {
	_, err := p.pool.Exec(ctx,
		`DELETE FROM chat_members WHERE group_id = $1 AND user_id = $2`, groupID, userID)
	return err
}

func (p *Postgres) IsMember(ctx context.Context, groupID, userID string) (bool, error) {
	var one int
	err := p.pool.QueryRow(ctx,
		`SELECT 1 FROM chat_members WHERE group_id = $1 AND user_id = $2`, groupID, userID,
	).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (p *Postgres) ListGroupsForUser(ctx context.Context, userID string) ([]ChatGroup, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT g.id, g.type, COALESCE(g.event_id,''), g.name, COALESCE(g.invite_code,''),
		       g.created_by, g.created_at,
		       (SELECT COUNT(*) FROM chat_members mc WHERE mc.group_id = g.id),
		       COALESCE((SELECT body FROM chat_messages lm WHERE lm.group_id = g.id ORDER BY lm.id DESC LIMIT 1), ''),
		       COALESCE((SELECT created_at FROM chat_messages lm WHERE lm.group_id = g.id ORDER BY lm.id DESC LIMIT 1), 0)
		FROM chat_groups g
		JOIN chat_members m ON m.group_id = g.id
		WHERE m.user_id = $1
		ORDER BY GREATEST(g.created_at, COALESCE((SELECT created_at FROM chat_messages lm WHERE lm.group_id = g.id ORDER BY lm.id DESC LIMIT 1), 0)) DESC`,
		userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChatGroup
	for rows.Next() {
		var g ChatGroup
		var createdAt, lastMsgAt int64
		var memberCount int64
		if err := rows.Scan(&g.ID, &g.Type, &g.EventID, &g.Name, &g.InviteCode,
			&g.CreatedBy, &createdAt, &memberCount, &g.LastMsgBody, &lastMsgAt); err != nil {
			return nil, err
		}
		g.MemberCount = int(memberCount)
		g.CreatedAt = time.Unix(createdAt, 0)
		if lastMsgAt > 0 {
			g.LastMsgAt = time.Unix(lastMsgAt, 0)
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (p *Postgres) InsertChatMessage(ctx context.Context, m ChatMessage) (int64, error) {
	var id int64
	err := p.pool.QueryRow(ctx,
		`INSERT INTO chat_messages (group_id, user_id, kind, body, created_at)
		 VALUES ($1,$2,$3,$4,$5) RETURNING id`,
		m.GroupID, m.UserID, m.Kind, m.Body, m.CreatedAt.Unix()).Scan(&id)
	return id, err
}

func (p *Postgres) ListChatUsers(ctx context.Context) ([]ChatUserAdmin, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT u.id, u.name, u.created_at,
		       (SELECT COUNT(*) FROM chat_members m WHERE m.user_id = u.id),
		       (SELECT COUNT(*) FROM chat_messages mm WHERE mm.user_id = u.id)
		FROM chat_users u
		ORDER BY u.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChatUserAdmin
	for rows.Next() {
		var u ChatUserAdmin
		var createdAt, groupCount, msgCount int64
		if err := rows.Scan(&u.ID, &u.Name, &createdAt, &groupCount, &msgCount); err != nil {
			return nil, err
		}
		u.GroupCount = int(groupCount)
		u.MessageCount = int(msgCount)
		u.CreatedAt = time.Unix(createdAt, 0)
		out = append(out, u)
	}
	return out, rows.Err()
}

func (p *Postgres) ListAllGroups(ctx context.Context) ([]ChatGroup, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT g.id, g.type, COALESCE(g.event_id,''), g.name, COALESCE(g.invite_code,''),
		       g.created_by, g.created_at,
		       (SELECT COUNT(*) FROM chat_members mc WHERE mc.group_id = g.id),
		       COALESCE((SELECT body FROM chat_messages lm WHERE lm.group_id = g.id ORDER BY lm.id DESC LIMIT 1), ''),
		       COALESCE((SELECT created_at FROM chat_messages lm WHERE lm.group_id = g.id ORDER BY lm.id DESC LIMIT 1), 0)
		FROM chat_groups g
		ORDER BY g.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChatGroup
	for rows.Next() {
		var g ChatGroup
		var createdAt, lastMsgAt, memberCount int64
		if err := rows.Scan(&g.ID, &g.Type, &g.EventID, &g.Name, &g.InviteCode,
			&g.CreatedBy, &createdAt, &memberCount, &g.LastMsgBody, &lastMsgAt); err != nil {
			return nil, err
		}
		g.MemberCount = int(memberCount)
		g.CreatedAt = time.Unix(createdAt, 0)
		if lastMsgAt > 0 {
			g.LastMsgAt = time.Unix(lastMsgAt, 0)
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (p *Postgres) DeleteChatUser(ctx context.Context, id string) error {
	if _, err := p.pool.Exec(ctx, `DELETE FROM chat_members WHERE user_id = $1`, id); err != nil {
		return err
	}
	_, err := p.pool.Exec(ctx, `DELETE FROM chat_users WHERE id = $1`, id)
	return err
}

func (p *Postgres) DeleteChatGroup(ctx context.Context, id string) error {
	if _, err := p.pool.Exec(ctx, `DELETE FROM chat_messages WHERE group_id = $1`, id); err != nil {
		return err
	}
	if _, err := p.pool.Exec(ctx, `DELETE FROM chat_members WHERE group_id = $1`, id); err != nil {
		return err
	}
	_, err := p.pool.Exec(ctx, `DELETE FROM chat_groups WHERE id = $1`, id)
	return err
}

func (p *Postgres) ListChatMessages(ctx context.Context, groupID string, beforeID int64, limit int) ([]ChatMessage, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT m.id, m.group_id, m.user_id, COALESCE(u.name, '?'), m.kind, m.body, m.created_at
		FROM chat_messages m
		LEFT JOIN chat_users u ON u.id = m.user_id
		WHERE m.group_id = $1 AND ($2 = 0 OR m.id < $2)
		ORDER BY m.id DESC
		LIMIT $3`,
		groupID, beforeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
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
