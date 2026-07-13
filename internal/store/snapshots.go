package store

import (
	"context"
	"fmt"
	"time"
)

// GrowthSnapshot is one locally persisted channel measurement. Capturing a
// snapshot reads Telegram metadata/history but never changes Telegram state.
type GrowthSnapshot struct {
	ID               int64     `json:"id"`
	ChatID           int64     `json:"chat_id"`
	Title            string    `json:"title"`
	Username         string    `json:"username,omitempty"`
	Subscribers      int       `json:"subscribers"`
	Posts            int       `json:"posts"`
	MaturePosts      int       `json:"mature_posts"`
	MedianViews      int       `json:"median_views"`
	MedianForwards   int       `json:"median_forwards"`
	MedianReactions  int       `json:"median_reactions"`
	WindowHours      int       `json:"window_hours"`
	MatureAfterHours int       `json:"mature_after_hours"`
	CapturedAt       time.Time `json:"captured_at"`
}

// SaveGrowthSnapshot persists one measurement in the local SQLite database.
func (s *Store) SaveGrowthSnapshot(ctx context.Context, snap GrowthSnapshot) (GrowthSnapshot, error) {
	if snap.ChatID <= 0 {
		return GrowthSnapshot{}, fmt.Errorf("chat_id must be positive")
	}
	if snap.Subscribers < 0 || snap.Posts < 0 || snap.MaturePosts < 0 ||
		snap.MedianViews < 0 || snap.MedianForwards < 0 || snap.MedianReactions < 0 {
		return GrowthSnapshot{}, fmt.Errorf("growth metrics must not be negative")
	}
	if snap.MaturePosts > snap.Posts {
		return GrowthSnapshot{}, fmt.Errorf("mature_posts must not exceed posts")
	}
	if snap.WindowHours <= 0 || snap.MatureAfterHours < 0 {
		return GrowthSnapshot{}, fmt.Errorf("window_hours must be positive and mature_after_hours must not be negative")
	}
	if snap.CapturedAt.IsZero() {
		snap.CapturedAt = time.Now().UTC()
	} else {
		snap.CapturedAt = snap.CapturedAt.UTC()
	}
	snap.CapturedAt = snap.CapturedAt.Truncate(time.Second)

	res, err := s.db.ExecContext(ctx, `
		INSERT INTO growth_snapshots(
		  chat_id, title, username, subscribers, posts, mature_posts,
		  median_views, median_forwards, median_reactions,
		  window_hours, mature_after_hours, captured_at
		) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		snap.ChatID, snap.Title, snap.Username, snap.Subscribers, snap.Posts, snap.MaturePosts,
		snap.MedianViews, snap.MedianForwards, snap.MedianReactions,
		snap.WindowHours, snap.MatureAfterHours, snap.CapturedAt.Unix())
	if err != nil {
		return GrowthSnapshot{}, fmt.Errorf("insert growth snapshot: %w", err)
	}
	snap.ID, _ = res.LastInsertId()
	return snap, nil
}

// ListGrowthSnapshots returns newest snapshots first. Days=0 means all time.
func (s *Store) ListGrowthSnapshots(ctx context.Context, chatID int64, days, limit int) ([]GrowthSnapshot, error) {
	if chatID <= 0 {
		return nil, fmt.Errorf("chat_id must be positive")
	}
	if days < 0 {
		return nil, fmt.Errorf("days must not be negative")
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}

	q := `SELECT id, chat_id, title, username, subscribers, posts, mature_posts,
	             median_views, median_forwards, median_reactions,
	             window_hours, mature_after_hours, captured_at
	        FROM growth_snapshots WHERE chat_id=?`
	args := []any{chatID}
	if days > 0 {
		q += ` AND captured_at>=?`
		args = append(args, time.Now().UTC().Add(-time.Duration(days)*24*time.Hour).Unix())
	}
	q += ` ORDER BY captured_at DESC, id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list growth snapshots: %w", err)
	}
	defer rows.Close()

	var out []GrowthSnapshot
	for rows.Next() {
		var snap GrowthSnapshot
		var capturedAt int64
		if err := rows.Scan(
			&snap.ID, &snap.ChatID, &snap.Title, &snap.Username, &snap.Subscribers,
			&snap.Posts, &snap.MaturePosts, &snap.MedianViews, &snap.MedianForwards,
			&snap.MedianReactions, &snap.WindowHours, &snap.MatureAfterHours, &capturedAt,
		); err != nil {
			return nil, err
		}
		snap.CapturedAt = time.Unix(capturedAt, 0).UTC()
		out = append(out, snap)
	}
	return out, rows.Err()
}
