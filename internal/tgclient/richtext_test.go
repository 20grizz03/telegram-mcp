package tgclient

import (
	"strings"
	"testing"
	"time"

	"github.com/gotd/td/tg"
)

func TestRenderRichMessage(t *testing.T) {
	message := tg.RichMessage{Blocks: []tg.PageBlockClass{
		&tg.PageBlockHeading1{Text: &tg.TextPlain{Text: "Rich heading"}},
		&tg.PageBlockParagraph{Text: &tg.TextConcat{Texts: []tg.RichTextClass{
			&tg.TextPlain{Text: "Native "},
			&tg.TextBold{Text: &tg.TextPlain{Text: "structured"}},
			&tg.TextPlain{Text: " message"},
		}}},
		&tg.PageBlockTable{
			Rows: []tg.PageTableRow{
				{Cells: []tg.PageTableCell{
					{Text: &tg.TextPlain{Text: "Feature"}},
					{Text: &tg.TextPlain{Text: "Status"}},
				}},
				{Cells: []tg.PageTableCell{
					{Text: &tg.TextPlain{Text: "Tables"}},
					{Text: &tg.TextPlain{Text: "ready"}},
				}},
			},
		},
		&tg.PageBlockBlockquote{
			Text: &tg.TextPlain{Text: "Technical check"},
		},
		&tg.PageBlockPreformatted{
			Text: &tg.TextPlain{Text: "publish_rich_post"},
		},
	}}

	got := renderRichMessage(message)
	for _, want := range []string{
		"Rich heading",
		"Native structured message",
		"| Feature | Status |",
		"| Tables | ready |",
		"> Technical check",
		"publish_rich_post",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("rendered rich message does not contain %q:\n%s", want, got)
		}
	}
}

func TestMapMessageUsesRichMessageText(t *testing.T) {
	client := &Client{loc: time.UTC}
	wire := &tg.Message{ID: 56, Date: 1_700_000_000}
	wire.SetRichMessage(tg.RichMessage{Blocks: []tg.PageBlockClass{
		&tg.PageBlockTitle{Text: &tg.TextPlain{Text: "Test Rich Text from MCP"}},
	}})

	got := client.mapMessage(wire, entityIndex{}, peerInfo{
		kind:      KindChannel,
		title:     "On IT Pulse Chat",
		channelID: 3834633286,
	}, 0)
	if got.Text != "Test Rich Text from MCP" {
		t.Fatalf("text = %q", got.Text)
	}
}
