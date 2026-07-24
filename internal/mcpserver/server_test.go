package mcpserver

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/20grizz03/telegram-mcp/internal/config"
	"github.com/20grizz03/telegram-mcp/internal/store"
	"github.com/20grizz03/telegram-mcp/internal/tgclient"
)

// connect spins up the server over an in-memory transport and returns a client
// session plus the backing store. Telegram is never dialed (only store-backed
// tools are exercised here).
func connect(t *testing.T) (*mcp.ClientSession, *store.Store) {
	t.Helper()
	home := t.TempDir()
	tc, err := tgclient.New(config.Config{AppID: 1, AppHash: "x", Home: home})
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(home + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	clientT, serverT := mcp.NewInMemoryTransports()
	ctx := context.Background()
	go func() { _ = Build(tc, st, false).Run(ctx, serverT) }()

	cl := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	cs, err := cl.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs, st
}

func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if res.IsError {
		t.Fatalf("%s returned tool error: %v", name, res.Content)
	}
	return res
}

func TestSearchChatTool(t *testing.T) {
	cs, st := connect(t)
	ctx := context.Background()

	chat := int64(1234567890)
	if err := st.UpsertChat(ctx, store.ChatMeta{ChatID: chat, Title: "ОМ: ВУ", Kind: "channel", IsForum: true}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertMessages(ctx, []store.Msg{
		{ChatID: chat, MsgID: 86694, TopicID: 1, Date: 1000, Sender: "Jul", Text: "пришлите ссылки на аккаунты"},
		{ChatID: chat, MsgID: 200, TopicID: 0, Date: 1001, Sender: "X", Text: "офтоп про погоду"},
	}); err != nil {
		t.Fatal(err)
	}

	// prefix match: "аккаунт" should find "аккаунты"
	res := call(t, cs, "search_chat", map[string]any{"query": "аккаунт", "chat_id": chat})
	var out searchChatOut
	raw, _ := json.Marshal(res.StructuredContent)
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.Count != 1 || len(out.Hits) != 1 {
		t.Fatalf("search = %+v, want 1 hit", out)
	}
	h := out.Hits[0]
	if h.MsgID != 86694 {
		t.Fatalf("wrong hit: %+v", h)
	}
	if h.Link != "https://t.me/c/1234567890/1/86694" {
		t.Fatalf("link = %q, want topic deep-link", h.Link)
	}
}

func TestPreferenceTools(t *testing.T) {
	cs, _ := connect(t)

	// save a global rule
	save := call(t, cs, "save_preference", map[string]any{"rule": "skip job spam"})
	saved, _ := json.Marshal(save.StructuredContent)
	if !strings.Contains(string(saved), "skip job spam") {
		t.Fatalf("save result missing rule: %s", saved)
	}

	// list -> count 1
	list := call(t, cs, "list_preferences", map[string]any{})
	var lout struct {
		Preferences []store.Preference `json:"preferences"`
		Count       int                `json:"count"`
	}
	raw, _ := json.Marshal(list.StructuredContent)
	if err := json.Unmarshal(raw, &lout); err != nil {
		t.Fatal(err)
	}
	if lout.Count != 1 || len(lout.Preferences) != 1 {
		t.Fatalf("list = %+v, want 1", lout)
	}
	id := lout.Preferences[0].ID

	// delete -> deleted true
	del := call(t, cs, "delete_preference", map[string]any{"id": id})
	draw, _ := json.Marshal(del.StructuredContent)
	if !strings.Contains(string(draw), "true") {
		t.Fatalf("delete result = %s, want deleted true", draw)
	}
}

func TestGrowthCRMToolsAreLocalOnly(t *testing.T) {
	cs, st := connect(t)
	ctx := context.Background()

	saved := call(t, cs, "save_partner", map[string]any{
		"username": "@example_channel", "title": "Example Channel", "status": "candidate", "audience_size": 4200,
	})
	var sout savePartnerOut
	raw, _ := json.Marshal(saved.StructuredContent)
	if err := json.Unmarshal(raw, &sout); err != nil {
		t.Fatal(err)
	}
	if sout.Partner.ID == 0 || sout.Partner.Title != "Example Channel" {
		t.Fatalf("save_partner = %+v", sout)
	}

	// Partial updates preserve fields not present in the call.
	updated := call(t, cs, "save_partner", map[string]any{
		"id": sout.Partner.ID, "status": "contacted",
	})
	raw, _ = json.Marshal(updated.StructuredContent)
	if err := json.Unmarshal(raw, &sout); err != nil {
		t.Fatal(err)
	}
	if sout.Partner.Status != "contacted" || sout.Partner.Title != "Example Channel" {
		t.Fatalf("partial update = %+v", sout.Partner)
	}

	created := call(t, cs, "create_outreach_draft", map[string]any{
		"partner_id": sout.Partner.ID,
		"text":       "Привет! Предлагаю взаимный репост.",
	})
	var dout outreachDraftOut
	raw, _ = json.Marshal(created.StructuredContent)
	if err := json.Unmarshal(raw, &dout); err != nil {
		t.Fatal(err)
	}
	if dout.Draft.Status != "draft" {
		t.Fatalf("create_outreach_draft = %+v", dout)
	}
	call(t, cs, "update_outreach_draft", map[string]any{"id": dout.Draft.ID, "status": "ready"})

	listed := call(t, cs, "list_outreach_drafts", map[string]any{
		"partner_id": sout.Partner.ID, "status": "ready",
	})
	var lout listOutreachDraftsOut
	raw, _ = json.Marshal(listed.StructuredContent)
	if err := json.Unmarshal(raw, &lout); err != nil {
		t.Fatal(err)
	}
	if lout.Count != 1 || len(lout.Drafts) != 1 {
		t.Fatalf("list_outreach_drafts = %+v", lout)
	}

	chatID := int64(1234567890)
	base := store.GrowthSnapshot{
		ChatID: chatID, Title: "Example Tech", Username: "exampletech",
		Posts: 8, MaturePosts: 6, MedianViews: 12, WindowHours: 168,
		MatureAfterHours: 72, CapturedAt: time.Now().UTC().Add(-24 * time.Hour),
	}
	base.Subscribers = 14
	if _, err := st.SaveGrowthSnapshot(ctx, base); err != nil {
		t.Fatal(err)
	}
	base.Subscribers = 17
	base.MedianViews = 15
	base.CapturedAt = time.Now().UTC()
	if _, err := st.SaveGrowthSnapshot(ctx, base); err != nil {
		t.Fatal(err)
	}

	growth := call(t, cs, "list_growth_snapshots", map[string]any{"chat_id": chatID})
	var gout listGrowthSnapshotsOut
	raw, _ = json.Marshal(growth.StructuredContent)
	if err := json.Unmarshal(raw, &gout); err != nil {
		t.Fatal(err)
	}
	if gout.Count != 2 || gout.Comparison == nil || gout.Comparison.SubscribersDelta != 3 {
		t.Fatalf("list_growth_snapshots = %+v", gout)
	}
}

func toolNames(t *testing.T, enableWrite bool) map[string]bool {
	t.Helper()
	home := t.TempDir()
	tc, err := tgclient.New(config.Config{AppID: 1, AppHash: "x", Home: home})
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(home + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	clientT, serverT := mcp.NewInMemoryTransports()
	ctx := context.Background()
	go func() { _ = Build(tc, st, enableWrite).Run(ctx, serverT) }()

	cl := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	cs, err := cl.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })

	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	set := map[string]bool{}
	for _, tl := range res.Tools {
		set[tl.Name] = true
	}
	return set
}

func TestWriteToolsGatedByFlag(t *testing.T) {
	write := []string{"create_invite_link", "create_shared_folder", "delete_scheduled_post", "edit_post", "edit_rich_post", "edit_scheduled_post", "forward_post", "pin_post", "publish_post", "publish_rich_post", "update_chat_description", "update_profile_bio"}

	off := toolNames(t, false)
	for _, n := range write {
		if off[n] {
			t.Errorf("write disabled: tool %q must be absent", n)
		}
	}

	on := toolNames(t, true)
	for _, n := range write {
		if !on[n] {
			t.Errorf("write enabled: tool %q must be present", n)
		}
	}
}

// TestToolsRegister verifies the MCP surface (tool names + inferred schemas)
// without touching Telegram: tgclient.New does not dial until Run is called.
func TestToolsRegister(t *testing.T) {
	home := t.TempDir()
	tc, err := tgclient.New(config.Config{AppID: 1, AppHash: "x", Home: home})
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(home + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	srv := Build(tc, st, false)

	clientT, serverT := mcp.NewInMemoryTransports()
	ctx := context.Background()

	go func() { _ = srv.Run(ctx, serverT) }()

	cl := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	cs, err := cl.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()

	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	var names []string
	for _, tl := range res.Tools {
		names = append(names, tl.Name)
	}
	sort.Strings(names)

	want := []string{
		"capture_growth_snapshot", "create_outreach_draft", "delete_preference", "download_media", "get_channel_profile",
		"list_channel_folders", "list_chats", "list_growth_snapshots", "list_invite_links", "list_outreach_drafts",
		"list_partners", "list_pinned_messages", "list_preferences", "list_scheduled_posts", "list_topics", "read_chat", "save_partner", "save_preference",
		"search_channels", "search_chat", "search_public_posts", "sync_chat", "update_outreach_draft",
	}
	if len(names) != len(want) {
		t.Fatalf("tools = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("tool[%d] = %q, want %q", i, names[i], want[i])
		}
	}
}

func TestMedianInt(t *testing.T) {
	for _, tc := range []struct {
		values []int
		want   int
	}{
		{nil, 0},
		{[]int{7}, 7},
		{[]int{9, 1, 5}, 5},
		{[]int{10, 2, 6, 4}, 5},
	} {
		if got := medianInt(tc.values); got != tc.want {
			t.Fatalf("medianInt(%v) = %d, want %d", tc.values, got, tc.want)
		}
	}
}
