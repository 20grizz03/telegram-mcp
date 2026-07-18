package store

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Watch is one chat (optionally a subset of forum topics) tracked for the daily
// digest. An empty TopicIDs means the whole chat. Focus is a per-chat brief that
// tells the summarizer what to extract and what to skip for this chat.
type Watch struct {
	ID        int64     `json:"id"`
	ChatID    int64     `json:"chat_id"`
	TopicIDs  []int     `json:"topic_ids,omitempty"`
	Label     string    `json:"label,omitempty"`
	Focus     string    `json:"focus,omitempty"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}

// AddWatch adds (or updates, keyed by chat_id) a watchlist entry and re-enables
// it. Returns the stored row.
func (s *Store) AddWatch(ctx context.Context, chatID int64, topicIDs []int, label, focus string) (Watch, error) {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO watchlist(chat_id, topic_ids, label, focus, enabled, created_at) VALUES(?,?,?,?,1,?)
		 ON CONFLICT(chat_id) DO UPDATE SET
		   topic_ids=excluded.topic_ids, label=excluded.label, focus=excluded.focus, enabled=1`,
		chatID, encodeTopicIDs(topicIDs), label, focus, time.Now().Unix())
	if err != nil {
		return Watch{}, fmt.Errorf("upsert watch: %w", err)
	}
	return s.getWatch(ctx, chatID)
}

func (s *Store) getWatch(ctx context.Context, chatID int64) (Watch, error) {
	var (
		w   Watch
		csv string
		en  int
		ts  int64
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, chat_id, topic_ids, label, focus, enabled, created_at FROM watchlist WHERE chat_id=?`, chatID).
		Scan(&w.ID, &w.ChatID, &csv, &w.Label, &w.Focus, &en, &ts)
	if err != nil {
		return Watch{}, err
	}
	w.TopicIDs = decodeTopicIDs(csv)
	w.Enabled = en != 0
	w.CreatedAt = time.Unix(ts, 0)
	return w, nil
}

// ListWatch returns watchlist entries ordered by id. With enabledOnly it skips
// disabled rows.
func (s *Store) ListWatch(ctx context.Context, enabledOnly bool) ([]Watch, error) {
	q := `SELECT id, chat_id, topic_ids, label, focus, enabled, created_at FROM watchlist`
	if enabledOnly {
		q += ` WHERE enabled=1`
	}
	q += ` ORDER BY id`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("query watchlist: %w", err)
	}
	defer rows.Close()

	var out []Watch
	for rows.Next() {
		var (
			w   Watch
			csv string
			en  int
			ts  int64
		)
		if err := rows.Scan(&w.ID, &w.ChatID, &csv, &w.Label, &w.Focus, &en, &ts); err != nil {
			return nil, err
		}
		w.TopicIDs = decodeTopicIDs(csv)
		w.Enabled = en != 0
		w.CreatedAt = time.Unix(ts, 0)
		out = append(out, w)
	}
	return out, rows.Err()
}

// DeleteWatch removes a watchlist entry by id. Returns false if no such id.
func (s *Store) DeleteWatch(ctx context.Context, id int64) (bool, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM watchlist WHERE id=?`, id)
	if err != nil {
		return false, fmt.Errorf("delete watch: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func encodeTopicIDs(ids []int) string {
	if len(ids) == 0 {
		return ""
	}
	parts := make([]string, len(ids))
	for i, n := range ids {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ",")
}

func decodeTopicIDs(csv string) []int {
	csv = strings.TrimSpace(csv)
	if csv == "" {
		return nil
	}
	var out []int
	for _, p := range strings.Split(csv, ",") {
		if p = strings.TrimSpace(p); p == "" {
			continue
		}
		if n, err := strconv.Atoi(p); err == nil {
			out = append(out, n)
		}
	}
	return out
}
