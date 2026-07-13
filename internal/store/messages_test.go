package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestMessageCacheAndSearch(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if !st.fts {
		t.Fatal("expected FTS5 to be available")
	}

	chat := int64(1234567890)
	msgs := []Msg{
		{ChatID: chat, MsgID: 100, TopicID: 1, Date: 1000, Sender: "Павел", Text: "Зарплата сеньора растёт"},
		{ChatID: chat, MsgID: 101, TopicID: 1, Date: 1001, Sender: "Исмаил", Text: "где получал премию?"},
		{ChatID: chat, MsgID: 102, TopicID: 0, Date: 1002, Sender: "Jul", Text: "пришлите ссылки на аккаунты"},
		{ChatID: 999, MsgID: 5, Date: 900, Sender: "X", Text: "зарплата в другом чате"},
	}
	if err := st.UpsertMessages(ctx, msgs); err != nil {
		t.Fatal(err)
	}

	// count scoped to chat
	if n, _ := st.CountMessages(ctx, chat); n != 3 {
		t.Fatalf("count(chat)=%d want 3", n)
	}

	// FTS, scoped to chat: "зарплата" -> only msg 100 (other is chat 999)
	hits, err := st.SearchMessages(ctx, &chat, "зарплата", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].MsgID != 100 {
		t.Fatalf("search(зарплата) = %+v, want msg 100 only", hits)
	}

	// global FTS: "зарплата" across all chats -> 2
	hits, _ = st.SearchMessages(ctx, nil, "зарплата", 10)
	if len(hits) != 2 {
		t.Fatalf("global search = %d hits, want 2", len(hits))
	}

	// idempotent upsert + edit: re-insert msg 100 with new text, FTS reflects it
	msgs[0].Text = "оклад джуна не меняется"
	if err := st.UpsertMessages(ctx, []Msg{msgs[0]}); err != nil {
		t.Fatal(err)
	}
	if n, _ := st.CountMessages(ctx, chat); n != 3 {
		t.Fatalf("count after re-upsert=%d want 3 (no dup)", n)
	}
	hits, _ = st.SearchMessages(ctx, &chat, "зарплата", 10)
	if len(hits) != 0 {
		t.Fatalf("stale FTS: 'зарплата' still matches after edit: %+v", hits)
	}
	hits, _ = st.SearchMessages(ctx, &chat, "оклад", 10)
	if len(hits) != 1 {
		t.Fatalf("edited text not searchable: %+v", hits)
	}

	// sync state round-trip
	if err := st.SetSyncState(ctx, chat, 1, 102); err != nil {
		t.Fatal(err)
	}
	if last, _ := st.GetSyncState(ctx, chat, 1); last != 102 {
		t.Fatalf("sync state = %d want 102", last)
	}
	if last, _ := st.GetSyncState(ctx, chat, 0); last != 0 {
		t.Fatalf("unrelated topic sync state = %d want 0", last)
	}

	// chat meta round-trip
	if err := st.UpsertChat(ctx, ChatMeta{ChatID: chat, Title: "ОМ: ВУ", Kind: "channel", IsForum: true}); err != nil {
		t.Fatal(err)
	}
	got, ok, _ := st.GetChat(ctx, chat)
	if !ok || got.Title != "ОМ: ВУ" || !got.IsForum {
		t.Fatalf("get chat = %+v ok=%v", got, ok)
	}
}
