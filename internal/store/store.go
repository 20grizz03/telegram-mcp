// Package store is the local SQLite persistence layer. For now it holds learned
// user preferences ("don't show me X"); phases 2B/2C extend the same database
// with a message cache + FTS index.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go driver, registered as "sqlite"
)

// Store wraps the SQLite database.
type Store struct {
	db  *sql.DB
	fts bool // true if the FTS5 index is available
}

// Preference is a single learned rule applied when summarizing.
type Preference struct {
	ID        int64     `json:"id"`
	Scope     string    `json:"scope"`             // "global" or "chat"
	ChatID    *int64    `json:"chat_id,omitempty"` // set when scope == "chat"
	Rule      string    `json:"rule"`              // the instruction, e.g. "skip job-spam"
	CreatedAt time.Time `json:"created_at"`
}

const schema = `
CREATE TABLE IF NOT EXISTS preferences (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    scope      TEXT NOT NULL DEFAULT 'global',
    chat_id    INTEGER,
    rule       TEXT NOT NULL,
    created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS chats (
    chat_id   INTEGER PRIMARY KEY,
    title     TEXT,
    kind      TEXT,
    username  TEXT,
    is_forum  INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS messages (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    chat_id     INTEGER NOT NULL,
    msg_id      INTEGER NOT NULL,
    topic_id    INTEGER NOT NULL DEFAULT 0,
    date        INTEGER NOT NULL,
    sender_id   INTEGER,
    sender_name TEXT,
    text        TEXT,
    reply_to    INTEGER NOT NULL DEFAULT 0,
    UNIQUE(chat_id, msg_id)
);
CREATE INDEX IF NOT EXISTS idx_messages_chat_date ON messages(chat_id, date);

CREATE TABLE IF NOT EXISTS sync_state (
    chat_id     INTEGER NOT NULL,
    topic_id    INTEGER NOT NULL DEFAULT 0,
    last_msg_id INTEGER NOT NULL DEFAULT 0,
    updated_at  INTEGER NOT NULL,
    PRIMARY KEY (chat_id, topic_id)
);

CREATE TABLE IF NOT EXISTS partners (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    chat_id        INTEGER UNIQUE,
    username       TEXT COLLATE NOCASE UNIQUE,
    title          TEXT NOT NULL DEFAULT '',
    contact        TEXT NOT NULL DEFAULT '',
    status         TEXT NOT NULL DEFAULT 'candidate',
    notes          TEXT NOT NULL DEFAULT '',
    terms          TEXT NOT NULL DEFAULT '',
    audience_size  INTEGER NOT NULL DEFAULT 0,
    median_views   INTEGER NOT NULL DEFAULT 0,
    next_action_at INTEGER,
    created_at     INTEGER NOT NULL,
    updated_at     INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_partners_status_action
    ON partners(status, next_action_at);

CREATE TABLE IF NOT EXISTS outreach_drafts (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    partner_id      INTEGER,
    target_chat_id  INTEGER,
    target_username TEXT NOT NULL DEFAULT '',
    text            TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'draft',
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL,
    FOREIGN KEY(partner_id) REFERENCES partners(id)
);
CREATE INDEX IF NOT EXISTS idx_outreach_drafts_status
    ON outreach_drafts(status, updated_at DESC);

CREATE TABLE IF NOT EXISTS growth_snapshots (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    chat_id          INTEGER NOT NULL,
    title            TEXT NOT NULL DEFAULT '',
    username         TEXT NOT NULL DEFAULT '',
    subscribers      INTEGER NOT NULL DEFAULT 0,
    posts             INTEGER NOT NULL DEFAULT 0,
    mature_posts      INTEGER NOT NULL DEFAULT 0,
    median_views      INTEGER NOT NULL DEFAULT 0,
    median_forwards   INTEGER NOT NULL DEFAULT 0,
    median_reactions  INTEGER NOT NULL DEFAULT 0,
    window_hours      INTEGER NOT NULL DEFAULT 0,
    mature_after_hours INTEGER NOT NULL DEFAULT 0,
    captured_at      INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_growth_snapshots_chat_time
    ON growth_snapshots(chat_id, captured_at DESC);
`

// ftsSchema is created separately so a sqlite build without FTS5 degrades
// gracefully (search falls back to LIKE). The triggers keep the external-content
// index in sync with the messages table.
const ftsSchema = `
CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(
    text,
    content='messages',
    content_rowid='id',
    tokenize='unicode61 remove_diacritics 2'
);
CREATE TRIGGER IF NOT EXISTS messages_ai AFTER INSERT ON messages BEGIN
    INSERT INTO messages_fts(rowid, text) VALUES (new.id, new.text);
END;
CREATE TRIGGER IF NOT EXISTS messages_ad AFTER DELETE ON messages BEGIN
    INSERT INTO messages_fts(messages_fts, rowid, text) VALUES('delete', old.id, old.text);
END;
CREATE TRIGGER IF NOT EXISTS messages_au AFTER UPDATE ON messages BEGIN
    INSERT INTO messages_fts(messages_fts, rowid, text) VALUES('delete', old.id, old.text);
    INSERT INTO messages_fts(rowid, text) VALUES (new.id, new.text);
END;
`

// Open opens (creating if needed) the database at path and runs migrations.
// WAL mode + a busy timeout let the CLI (`tgmcp pref`) and a running `serve`
// share the file without SQLITE_BUSY errors.
func Open(path string) (*Store, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	db.SetMaxOpenConns(1) // sqlite: serialize writers within this process

	ctx := context.Background()
	if _, err := db.ExecContext(ctx, schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	s := &Store{db: db}
	if _, err := db.ExecContext(ctx, ftsSchema); err != nil {
		// FTS5 not compiled in: keep going, search falls back to LIKE.
		fmt.Fprintf(os.Stderr, "tg-mcp: FTS5 unavailable, using LIKE search: %v\n", err)
	} else {
		s.fts = true
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// AddPreference stores a rule. A non-nil chatID makes it chat-scoped; otherwise
// it is global.
func (s *Store) AddPreference(ctx context.Context, rule string, chatID *int64) (Preference, error) {
	scope := "global"
	if chatID != nil {
		scope = "chat"
	}
	now := time.Now()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO preferences(scope, chat_id, rule, created_at) VALUES(?,?,?,?)`,
		scope, chatID, rule, now.Unix())
	if err != nil {
		return Preference{}, fmt.Errorf("insert preference: %w", err)
	}
	id, _ := res.LastInsertId()
	return Preference{ID: id, Scope: scope, ChatID: chatID, Rule: rule, CreatedAt: now}, nil
}

// ListPreferences returns rules. With a non-nil chatID it returns global rules
// plus the rules scoped to that chat; with nil it returns every rule.
func (s *Store) ListPreferences(ctx context.Context, chatID *int64) ([]Preference, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if chatID != nil {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, scope, chat_id, rule, created_at FROM preferences
			 WHERE scope='global' OR chat_id=? ORDER BY id`, *chatID)
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, scope, chat_id, rule, created_at FROM preferences ORDER BY id`)
	}
	if err != nil {
		return nil, fmt.Errorf("query preferences: %w", err)
	}
	defer rows.Close()

	var out []Preference
	for rows.Next() {
		var (
			p   Preference
			cid sql.NullInt64
			ts  int64
		)
		if err := rows.Scan(&p.ID, &p.Scope, &cid, &p.Rule, &ts); err != nil {
			return nil, err
		}
		if cid.Valid {
			v := cid.Int64
			p.ChatID = &v
		}
		p.CreatedAt = time.Unix(ts, 0)
		out = append(out, p)
	}
	return out, rows.Err()
}

// DeletePreference removes a rule by id. Returns false if no such id existed.
func (s *Store) DeletePreference(ctx context.Context, id int64) (bool, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM preferences WHERE id=?`, id)
	if err != nil {
		return false, fmt.Errorf("delete preference: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ---- message cache (phase 2B) ----

// Msg is a cached chat message.
type Msg struct {
	ChatID   int64
	MsgID    int
	TopicID  int
	Date     int64 // unix seconds
	SenderID int64
	Sender   string
	Text     string
	ReplyTo  int
}

// ChatMeta is cached chat metadata, enough to rebuild deep links offline.
type ChatMeta struct {
	ChatID   int64  `json:"chat_id"`
	Title    string `json:"title"`
	Kind     string `json:"kind"`
	Username string `json:"username,omitempty"`
	IsForum  bool   `json:"is_forum"`
}

// SearchHit is one full-text search result.
type SearchHit struct {
	ChatID  int64  `json:"chat_id"`
	MsgID   int    `json:"msg_id"`
	TopicID int    `json:"topic_id,omitempty"`
	Date    int64  `json:"date"`
	Sender  string `json:"sender"`
	Text    string `json:"text"`
}

// UpsertChat stores/updates chat metadata.
func (s *Store) UpsertChat(ctx context.Context, c ChatMeta) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO chats(chat_id, title, kind, username, is_forum) VALUES(?,?,?,?,?)
		 ON CONFLICT(chat_id) DO UPDATE SET
		   title=excluded.title, kind=excluded.kind,
		   username=excluded.username, is_forum=excluded.is_forum`,
		c.ChatID, c.Title, c.Kind, c.Username, boolToInt(c.IsForum))
	if err != nil {
		return fmt.Errorf("upsert chat: %w", err)
	}
	return nil
}

// GetChat returns cached chat metadata.
func (s *Store) GetChat(ctx context.Context, chatID int64) (ChatMeta, bool, error) {
	var (
		c     ChatMeta
		forum int
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT chat_id, title, kind, username, is_forum FROM chats WHERE chat_id=?`, chatID).
		Scan(&c.ChatID, &c.Title, &c.Kind, &c.Username, &forum)
	if err == sql.ErrNoRows {
		return ChatMeta{}, false, nil
	}
	if err != nil {
		return ChatMeta{}, false, err
	}
	c.IsForum = forum != 0
	return c, true, nil
}

// UpsertMessages inserts/updates a batch of messages in one transaction. The FTS
// index (if present) is maintained by triggers.
func (s *Store) UpsertMessages(ctx context.Context, msgs []Msg) error {
	if len(msgs) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO messages(chat_id, msg_id, topic_id, date, sender_id, sender_name, text, reply_to)
		 VALUES(?,?,?,?,?,?,?,?)
		 ON CONFLICT(chat_id, msg_id) DO UPDATE SET
		   date=excluded.date, sender_id=excluded.sender_id, sender_name=excluded.sender_name,
		   text=excluded.text, reply_to=excluded.reply_to,
		   topic_id=CASE WHEN excluded.topic_id!=0 THEN excluded.topic_id ELSE topic_id END`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, m := range msgs {
		if _, err := stmt.ExecContext(ctx,
			m.ChatID, m.MsgID, m.TopicID, m.Date, m.SenderID, m.Sender, m.Text, m.ReplyTo); err != nil {
			return fmt.Errorf("upsert message %d/%d: %w", m.ChatID, m.MsgID, err)
		}
	}
	return tx.Commit()
}

// CountMessages returns how many messages of a chat are cached.
func (s *Store) CountMessages(ctx context.Context, chatID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM messages WHERE chat_id=?`, chatID).Scan(&n)
	return n, err
}

// GetSyncState returns the highest cached msg_id for a chat/topic (0 if never synced).
func (s *Store) GetSyncState(ctx context.Context, chatID int64, topicID int) (int, error) {
	var last int
	err := s.db.QueryRowContext(ctx,
		`SELECT last_msg_id FROM sync_state WHERE chat_id=? AND topic_id=?`, chatID, topicID).Scan(&last)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return last, err
}

// SetSyncState records the highest cached msg_id for a chat/topic.
func (s *Store) SetSyncState(ctx context.Context, chatID int64, topicID, lastMsgID int) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sync_state(chat_id, topic_id, last_msg_id, updated_at) VALUES(?,?,?,?)
		 ON CONFLICT(chat_id, topic_id) DO UPDATE SET
		   last_msg_id=excluded.last_msg_id, updated_at=excluded.updated_at`,
		chatID, topicID, lastMsgID, time.Now().Unix())
	return err
}

// SearchMessages runs a full-text query, optionally scoped to one chat, newest
// first. Falls back to a LIKE scan when FTS5 is unavailable.
func (s *Store) SearchMessages(ctx context.Context, chatID *int64, query string, limit int) ([]SearchHit, error) {
	if limit <= 0 {
		limit = 50
	}
	var (
		rows *sql.Rows
		err  error
	)
	if s.fts {
		q := `SELECT m.chat_id, m.msg_id, m.topic_id, m.date, m.sender_name, m.text
		      FROM messages_fts f JOIN messages m ON m.id = f.rowid
		      WHERE f.text MATCH ?`
		args := []any{ftsPrefixQuery(query)}
		if chatID != nil {
			q += ` AND m.chat_id = ?`
			args = append(args, *chatID)
		}
		q += ` ORDER BY m.date DESC LIMIT ?`
		args = append(args, limit)
		rows, err = s.db.QueryContext(ctx, q, args...)
	} else {
		q := `SELECT chat_id, msg_id, topic_id, date, sender_name, text
		      FROM messages WHERE text LIKE ?`
		args := []any{"%" + query + "%"}
		if chatID != nil {
			q += ` AND chat_id = ?`
			args = append(args, *chatID)
		}
		q += ` ORDER BY date DESC LIMIT ?`
		args = append(args, limit)
		rows, err = s.db.QueryContext(ctx, q, args...)
	}
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	defer rows.Close()

	var out []SearchHit
	for rows.Next() {
		var (
			h      SearchHit
			sender sql.NullString
		)
		if err := rows.Scan(&h.ChatID, &h.MsgID, &h.TopicID, &h.Date, &sender, &h.Text); err != nil {
			return nil, err
		}
		h.Sender = sender.String
		out = append(out, h)
	}
	return out, rows.Err()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ftsPrefixQuery turns a plain query into an FTS5 prefix query so "зарплат"
// matches "зарплата/зарплату/зарплаты". Each whitespace-separated term gets a
// trailing '*'; terms are implicitly AND-ed. Already-quoted/operator queries are
// passed through untouched.
func ftsPrefixQuery(q string) string {
	q = strings.TrimSpace(q)
	if q == "" {
		return q
	}
	if strings.ContainsAny(q, `"*()`) {
		return q // caller used explicit FTS5 syntax
	}
	terms := strings.Fields(q)
	for i, t := range terms {
		terms[i] = `"` + strings.ReplaceAll(t, `"`, "") + `"` + "*"
	}
	return strings.Join(terms, " ")
}
