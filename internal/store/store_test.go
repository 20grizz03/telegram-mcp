package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestPreferencesCRUD(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	// global rule
	g, err := st.AddPreference(ctx, "skip job-spam", nil)
	if err != nil {
		t.Fatal(err)
	}
	if g.Scope != "global" || g.ChatID != nil {
		t.Fatalf("global pref wrong: %+v", g)
	}

	// chat-scoped rule
	chatID := int64(12345)
	c, err := st.AddPreference(ctx, "focus on salary numbers", &chatID)
	if err != nil {
		t.Fatal(err)
	}
	if c.Scope != "chat" || c.ChatID == nil || *c.ChatID != chatID {
		t.Fatalf("chat pref wrong: %+v", c)
	}

	// list for this chat: global + chat rule = 2
	got, err := st.ListPreferences(ctx, &chatID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("list(chat) = %d, want 2", len(got))
	}

	// list for a different chat: only global = 1
	other := int64(999)
	got, err = st.ListPreferences(ctx, &other)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Scope != "global" {
		t.Fatalf("list(other) = %+v, want only global", got)
	}

	// delete chat rule
	ok, err := st.DeletePreference(ctx, c.ID)
	if err != nil || !ok {
		t.Fatalf("delete = %v, %v", ok, err)
	}
	ok, _ = st.DeletePreference(ctx, c.ID) // second delete -> false
	if ok {
		t.Fatal("second delete should report false")
	}
}
