package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestPartnerAndOutreachDraftWorkflow(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	next := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)
	p, err := st.SavePartner(ctx, Partner{
		Username:     "@example_channel",
		Title:        "Example Channel",
		Contact:      "editor",
		Status:       "candidate",
		AudienceSize: 4200,
		MedianViews:  1300,
		NextActionAt: &next,
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.ID == 0 || p.Username != "example_channel" || p.NextActionAt == nil || !p.NextActionAt.Equal(next) {
		t.Fatalf("created partner = %+v", p)
	}

	// A later pass may discover the numeric chat id. Falling back to username
	// must enrich the same row rather than create a duplicate.
	chatID := int64(123456)
	updated, err := st.SavePartner(ctx, Partner{
		ChatID:       &chatID,
		Username:     "example_channel",
		Title:        "Example Channel",
		Contact:      "editor",
		Status:       "contacted",
		AudienceSize: 4300,
		MedianViews:  1400,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != p.ID || updated.ChatID == nil || *updated.ChatID != chatID {
		t.Fatalf("updated partner = %+v, original id %d", updated, p.ID)
	}
	partners, err := st.ListPartners(ctx, "contacted", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(partners) != 1 || partners[0].ID != p.ID {
		t.Fatalf("partners = %+v", partners)
	}

	draft, err := st.CreateOutreachDraft(ctx, OutreachDraft{
		PartnerID:      &p.ID,
		TargetUsername: "@example_channel",
		Text:           "Привет! Предлагаю взаимный репост.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if draft.Status != "draft" || draft.TargetUsername != "example_channel" {
		t.Fatalf("draft = %+v", draft)
	}
	draft, err = st.UpdateOutreachDraft(ctx, draft.ID, "", "ready")
	if err != nil {
		t.Fatal(err)
	}
	if draft.Status != "ready" || draft.Text == "" {
		t.Fatalf("updated draft = %+v", draft)
	}
	if _, err := st.UpdateOutreachDraft(ctx, draft.ID, "", "sent"); err == nil {
		t.Fatal("sent status must be rejected: the outbox is local-only")
	}
	drafts, err := st.ListOutreachDrafts(ctx, "ready", &p.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(drafts) != 1 || drafts[0].ID != draft.ID {
		t.Fatalf("drafts = %+v", drafts)
	}
}

func TestGrowthValidation(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if _, err := st.SavePartner(ctx, Partner{Status: "unknown"}); err == nil {
		t.Fatal("unknown partner status must fail")
	}
	if _, err := st.CreateOutreachDraft(ctx, OutreachDraft{Text: "  "}); err == nil {
		t.Fatal("empty draft must fail")
	}
}

func TestGrowthSnapshots(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	chatID := int64(1234567890)
	firstAt := time.Now().UTC().Add(-48 * time.Hour).Truncate(time.Second)
	secondAt := firstAt.Add(24 * time.Hour)
	for _, snap := range []GrowthSnapshot{
		{
			ChatID: chatID, Title: "Example Tech", Username: "exampletech",
			Subscribers: 14, Posts: 7, MaturePosts: 5, MedianViews: 11,
			MedianForwards: 1, MedianReactions: 2, WindowHours: 168,
			MatureAfterHours: 72, CapturedAt: firstAt,
		},
		{
			ChatID: chatID, Title: "Example Tech", Username: "exampletech",
			Subscribers: 17, Posts: 8, MaturePosts: 6, MedianViews: 13,
			MedianForwards: 2, MedianReactions: 3, WindowHours: 168,
			MatureAfterHours: 72, CapturedAt: secondAt,
		},
	} {
		if _, err := st.SaveGrowthSnapshot(ctx, snap); err != nil {
			t.Fatal(err)
		}
	}

	got, err := st.ListGrowthSnapshots(ctx, chatID, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Subscribers != 17 || got[1].Subscribers != 14 {
		t.Fatalf("snapshots = %+v", got)
	}
	if !got[0].CapturedAt.Equal(secondAt) || got[0].MedianViews != 13 {
		t.Fatalf("latest snapshot = %+v", got[0])
	}

	if _, err := st.SaveGrowthSnapshot(ctx, GrowthSnapshot{ChatID: chatID, WindowHours: 0}); err == nil {
		t.Fatal("zero window must fail")
	}
}
