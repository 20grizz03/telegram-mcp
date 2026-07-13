package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Partner is a channel or contact considered for cross-promotion. It is kept
// locally and never triggers Telegram activity on its own.
type Partner struct {
	ID           int64      `json:"id"`
	ChatID       *int64     `json:"chat_id,omitempty"`
	Username     string     `json:"username,omitempty"`
	Title        string     `json:"title"`
	Contact      string     `json:"contact,omitempty"`
	Status       string     `json:"status"`
	Notes        string     `json:"notes,omitempty"`
	Terms        string     `json:"terms,omitempty"`
	AudienceSize int        `json:"audience_size,omitempty"`
	MedianViews  int        `json:"median_views,omitempty"`
	NextActionAt *time.Time `json:"next_action_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

var partnerStatuses = map[string]bool{
	"candidate":   true,
	"contacted":   true,
	"negotiating": true,
	"agreed":      true,
	"active":      true,
	"paused":      true,
	"declined":    true,
}

// SavePartner creates or updates a partner. When ID is absent, an existing
// row is reused by chat_id or username so repeated research does not create
// duplicates.
func (s *Store) SavePartner(ctx context.Context, p Partner) (Partner, error) {
	p.Username = strings.TrimPrefix(strings.TrimSpace(p.Username), "@")
	p.Title = strings.TrimSpace(p.Title)
	p.Contact = strings.TrimSpace(p.Contact)
	p.Status = strings.ToLower(strings.TrimSpace(p.Status))
	p.Notes = strings.TrimSpace(p.Notes)
	p.Terms = strings.TrimSpace(p.Terms)
	if p.Status == "" {
		p.Status = "candidate"
	}
	if !partnerStatuses[p.Status] {
		return Partner{}, fmt.Errorf("invalid partner status %q", p.Status)
	}
	if p.AudienceSize < 0 || p.MedianViews < 0 {
		return Partner{}, fmt.Errorf("audience_size and median_views must not be negative")
	}
	if p.ID == 0 {
		var existingID int64
		if p.ChatID != nil {
			err := s.db.QueryRowContext(ctx, `SELECT id FROM partners WHERE chat_id=?`, *p.ChatID).Scan(&existingID)
			if err != nil && err != sql.ErrNoRows {
				return Partner{}, fmt.Errorf("find partner by chat id: %w", err)
			}
		}
		if existingID == 0 && p.Username != "" {
			err := s.db.QueryRowContext(ctx, `SELECT id FROM partners WHERE username=? COLLATE NOCASE`, p.Username).Scan(&existingID)
			if err != nil && err != sql.ErrNoRows {
				return Partner{}, fmt.Errorf("find partner by username: %w", err)
			}
		}
		p.ID = existingID
	}

	now := time.Now().UTC().Truncate(time.Second)
	nextAction := nullableUnix(p.NextActionAt)
	if p.ID == 0 {
		res, err := s.db.ExecContext(ctx, `
			INSERT INTO partners(
			  chat_id, username, title, contact, status, notes, terms,
			  audience_size, median_views, next_action_at, created_at, updated_at
			) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
			p.ChatID, nullableString(p.Username), p.Title, p.Contact, p.Status, p.Notes, p.Terms,
			p.AudienceSize, p.MedianViews, nextAction, now.Unix(), now.Unix())
		if err != nil {
			return Partner{}, fmt.Errorf("insert partner: %w", err)
		}
		p.ID, _ = res.LastInsertId()
	} else {
		res, err := s.db.ExecContext(ctx, `
			UPDATE partners SET
			  chat_id=?, username=?, title=?, contact=?, status=?, notes=?, terms=?,
			  audience_size=?, median_views=?, next_action_at=?, updated_at=?
			WHERE id=?`,
			p.ChatID, nullableString(p.Username), p.Title, p.Contact, p.Status, p.Notes, p.Terms,
			p.AudienceSize, p.MedianViews, nextAction, now.Unix(), p.ID)
		if err != nil {
			return Partner{}, fmt.Errorf("update partner: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return Partner{}, fmt.Errorf("partner %d not found", p.ID)
		}
	}
	return s.getPartner(ctx, p.ID)
}

// ListPartners returns partners ordered by the next action and latest update.
func (s *Store) ListPartners(ctx context.Context, status string, limit int) ([]Partner, error) {
	status = strings.ToLower(strings.TrimSpace(status))
	if status != "" && !partnerStatuses[status] {
		return nil, fmt.Errorf("invalid partner status %q", status)
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}

	q := partnerSelect
	args := []any{}
	if status != "" {
		q += ` WHERE status=?`
		args = append(args, status)
	}
	q += ` ORDER BY next_action_at IS NULL, next_action_at, updated_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list partners: %w", err)
	}
	defer rows.Close()

	var out []Partner
	for rows.Next() {
		p, err := scanPartner(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

const partnerSelect = `SELECT id, chat_id, username, title, contact, status, notes, terms,
       audience_size, median_views, next_action_at, created_at, updated_at FROM partners`

func (s *Store) getPartner(ctx context.Context, id int64) (Partner, error) {
	p, err := scanPartner(s.db.QueryRowContext(ctx, partnerSelect+` WHERE id=?`, id))
	if err == sql.ErrNoRows {
		return Partner{}, fmt.Errorf("partner %d not found", id)
	}
	return p, err
}

// GetPartner returns one local partner by id.
func (s *Store) GetPartner(ctx context.Context, id int64) (Partner, error) {
	return s.getPartner(ctx, id)
}

// FindPartner looks up a local partner by chat id first, then username.
func (s *Store) FindPartner(ctx context.Context, chatID *int64, username string) (Partner, bool, error) {
	var id int64
	if chatID != nil {
		err := s.db.QueryRowContext(ctx, `SELECT id FROM partners WHERE chat_id=?`, *chatID).Scan(&id)
		if err != nil && err != sql.ErrNoRows {
			return Partner{}, false, fmt.Errorf("find partner by chat id: %w", err)
		}
	}
	username = strings.TrimPrefix(strings.TrimSpace(username), "@")
	if id == 0 && username != "" {
		err := s.db.QueryRowContext(ctx, `SELECT id FROM partners WHERE username=? COLLATE NOCASE`, username).Scan(&id)
		if err != nil && err != sql.ErrNoRows {
			return Partner{}, false, fmt.Errorf("find partner by username: %w", err)
		}
	}
	if id == 0 {
		return Partner{}, false, nil
	}
	p, err := s.getPartner(ctx, id)
	return p, err == nil, err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanPartner(row scanner) (Partner, error) {
	var (
		p          Partner
		chatID     sql.NullInt64
		username   sql.NullString
		nextAction sql.NullInt64
		createdAt  int64
		updatedAt  int64
	)
	if err := row.Scan(
		&p.ID, &chatID, &username, &p.Title, &p.Contact, &p.Status, &p.Notes, &p.Terms,
		&p.AudienceSize, &p.MedianViews, &nextAction, &createdAt, &updatedAt,
	); err != nil {
		return Partner{}, err
	}
	if chatID.Valid {
		p.ChatID = &chatID.Int64
	}
	p.Username = username.String
	if nextAction.Valid {
		v := time.Unix(nextAction.Int64, 0).UTC()
		p.NextActionAt = &v
	}
	p.CreatedAt = time.Unix(createdAt, 0).UTC()
	p.UpdatedAt = time.Unix(updatedAt, 0).UTC()
	return p, nil
}

func nullableUnix(t *time.Time) any {
	if t == nil || t.IsZero() {
		return nil
	}
	return t.UTC().Unix()
}

func nullableString(v string) any {
	if v == "" {
		return nil
	}
	return v
}
