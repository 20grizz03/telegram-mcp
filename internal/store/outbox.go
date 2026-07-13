package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// OutreachDraft is a local-only draft for contacting a potential partner.
// There is deliberately no "sent" state: delivery is outside the outbox.
type OutreachDraft struct {
	ID             int64     `json:"id"`
	PartnerID      *int64    `json:"partner_id,omitempty"`
	TargetChatID   *int64    `json:"target_chat_id,omitempty"`
	TargetUsername string    `json:"target_username,omitempty"`
	Text           string    `json:"text"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

var draftStatuses = map[string]bool{
	"draft":    true,
	"ready":    true,
	"archived": true,
}

// CreateOutreachDraft stores text locally without contacting Telegram.
func (s *Store) CreateOutreachDraft(ctx context.Context, d OutreachDraft) (OutreachDraft, error) {
	d.Text = strings.TrimSpace(d.Text)
	d.TargetUsername = strings.TrimPrefix(strings.TrimSpace(d.TargetUsername), "@")
	if d.Text == "" {
		return OutreachDraft{}, fmt.Errorf("draft text must not be empty")
	}
	if d.Status == "" {
		d.Status = "draft"
	}
	d.Status = strings.ToLower(strings.TrimSpace(d.Status))
	if !draftStatuses[d.Status] {
		return OutreachDraft{}, fmt.Errorf("invalid draft status %q", d.Status)
	}
	if d.PartnerID != nil {
		var exists int
		if err := s.db.QueryRowContext(ctx, `SELECT 1 FROM partners WHERE id=?`, *d.PartnerID).Scan(&exists); err != nil {
			if err == sql.ErrNoRows {
				return OutreachDraft{}, fmt.Errorf("partner %d not found", *d.PartnerID)
			}
			return OutreachDraft{}, fmt.Errorf("check partner: %w", err)
		}
	}

	now := time.Now().UTC().Truncate(time.Second)
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO outreach_drafts(
		  partner_id, target_chat_id, target_username, text, status, created_at, updated_at
		) VALUES(?,?,?,?,?,?,?)`,
		d.PartnerID, d.TargetChatID, d.TargetUsername, d.Text, d.Status, now.Unix(), now.Unix())
	if err != nil {
		return OutreachDraft{}, fmt.Errorf("insert outreach draft: %w", err)
	}
	d.ID, _ = res.LastInsertId()
	return s.getOutreachDraft(ctx, d.ID)
}

// UpdateOutreachDraft changes local text/status. Allowed states intentionally
// stop at "ready"; sending requires a separate, explicitly authorized flow.
func (s *Store) UpdateOutreachDraft(ctx context.Context, id int64, text, status string) (OutreachDraft, error) {
	current, err := s.getOutreachDraft(ctx, id)
	if err != nil {
		return OutreachDraft{}, err
	}
	if strings.TrimSpace(text) != "" {
		current.Text = strings.TrimSpace(text)
	}
	if strings.TrimSpace(status) != "" {
		current.Status = strings.ToLower(strings.TrimSpace(status))
	}
	if !draftStatuses[current.Status] {
		return OutreachDraft{}, fmt.Errorf("invalid draft status %q", current.Status)
	}

	now := time.Now().UTC().Truncate(time.Second)
	_, err = s.db.ExecContext(ctx, `UPDATE outreach_drafts SET text=?, status=?, updated_at=? WHERE id=?`,
		current.Text, current.Status, now.Unix(), id)
	if err != nil {
		return OutreachDraft{}, fmt.Errorf("update outreach draft: %w", err)
	}
	return s.getOutreachDraft(ctx, id)
}

// ListOutreachDrafts returns locally stored drafts, newest first.
func (s *Store) ListOutreachDrafts(ctx context.Context, status string, partnerID *int64, limit int) ([]OutreachDraft, error) {
	status = strings.ToLower(strings.TrimSpace(status))
	if status != "" && !draftStatuses[status] {
		return nil, fmt.Errorf("invalid draft status %q", status)
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}

	q := outreachDraftSelect + ` WHERE 1=1`
	args := []any{}
	if status != "" {
		q += ` AND status=?`
		args = append(args, status)
	}
	if partnerID != nil {
		q += ` AND partner_id=?`
		args = append(args, *partnerID)
	}
	q += ` ORDER BY updated_at DESC, id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list outreach drafts: %w", err)
	}
	defer rows.Close()

	var out []OutreachDraft
	for rows.Next() {
		d, err := scanOutreachDraft(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

const outreachDraftSelect = `SELECT id, partner_id, target_chat_id, target_username,
       text, status, created_at, updated_at FROM outreach_drafts`

func (s *Store) getOutreachDraft(ctx context.Context, id int64) (OutreachDraft, error) {
	d, err := scanOutreachDraft(s.db.QueryRowContext(ctx, outreachDraftSelect+` WHERE id=?`, id))
	if err == sql.ErrNoRows {
		return OutreachDraft{}, fmt.Errorf("outreach draft %d not found", id)
	}
	return d, err
}

func scanOutreachDraft(row scanner) (OutreachDraft, error) {
	var (
		d            OutreachDraft
		partnerID    sql.NullInt64
		targetChatID sql.NullInt64
		createdAt    int64
		updatedAt    int64
	)
	if err := row.Scan(
		&d.ID, &partnerID, &targetChatID, &d.TargetUsername,
		&d.Text, &d.Status, &createdAt, &updatedAt,
	); err != nil {
		return OutreachDraft{}, err
	}
	if partnerID.Valid {
		d.PartnerID = &partnerID.Int64
	}
	if targetChatID.Valid {
		d.TargetChatID = &targetChatID.Int64
	}
	d.CreatedAt = time.Unix(createdAt, 0).UTC()
	d.UpdatedAt = time.Unix(updatedAt, 0).UTC()
	return d, nil
}
