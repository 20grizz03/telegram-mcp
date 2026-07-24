package tgclient

import (
	"fmt"
	"strings"

	"github.com/gotd/td/tg"
)

// renderRichMessage produces a compact plain-text representation for MCP
// clients. Telegram renders the original blocks natively; this fallback makes
// rich posts searchable and lets read_chat verify their actual content.
func renderRichMessage(message tg.RichMessage) string {
	return renderPageBlocks(message.Blocks)
}

func renderPageBlocks(blocks []tg.PageBlockClass) string {
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if text := strings.TrimSpace(renderPageBlock(block)); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n\n")
}

func renderPageBlock(block tg.PageBlockClass) string {
	switch value := block.(type) {
	case *tg.PageBlockPreformatted:
		return renderRichText(value.Text)
	case *tg.PageBlockBlockquote:
		return renderQuote(renderRichText(value.Text), renderRichText(value.Caption))
	case *tg.PageBlockPullquote:
		return renderQuote(renderRichText(value.Text), renderRichText(value.Caption))
	case *tg.PageBlockList:
		return renderList(value.Items)
	case *tg.PageBlockOrderedList:
		return renderOrderedList(value.Items)
	case *tg.PageBlockTable:
		return renderTable(value)
	case *tg.PageBlockDivider:
		return "---"
	case *tg.PageBlockMath:
		return value.Source
	case *tg.PageBlockPhoto:
		return "[photo]"
	case *tg.PageBlockVideo:
		return "[video]"
	case *tg.PageBlockAudio:
		return "[audio]"
	}

	if textBlock, ok := block.(interface {
		GetText() tg.RichTextClass
	}); ok {
		return renderRichText(textBlock.GetText())
	}
	if nestedBlock, ok := block.(interface {
		GetBlocks() []tg.PageBlockClass
	}); ok {
		return renderPageBlocks(nestedBlock.GetBlocks())
	}
	return ""
}

func renderRichText(text tg.RichTextClass) string {
	switch value := text.(type) {
	case nil, *tg.TextEmpty:
		return ""
	case *tg.TextPlain:
		return value.Text
	case *tg.TextConcat:
		var builder strings.Builder
		for _, child := range value.Texts {
			builder.WriteString(renderRichText(child))
		}
		return builder.String()
	case *tg.TextMath:
		return value.Source
	case *tg.TextCustomEmoji:
		return value.Alt
	case *tg.TextImage:
		return "[image]"
	}

	if wrapper, ok := text.(interface {
		GetText() tg.RichTextClass
	}); ok {
		return renderRichText(wrapper.GetText())
	}
	return ""
}

func renderQuote(text, caption string) string {
	content := strings.TrimSpace(text)
	if caption = strings.TrimSpace(caption); caption != "" {
		if content != "" {
			content += "\n— " + caption
		} else {
			content = caption
		}
	}
	if content == "" {
		return ""
	}

	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lines[i] = "> " + line
	}
	return strings.Join(lines, "\n")
}

func renderList(items []tg.PageListItemClass) string {
	lines := make([]string, 0, len(items))
	for _, item := range items {
		text := renderListItem(item)
		if text != "" {
			lines = append(lines, "- "+indentContinuation(text, "  "))
		}
	}
	return strings.Join(lines, "\n")
}

func renderListItem(item tg.PageListItemClass) string {
	switch value := item.(type) {
	case *tg.PageListItemText:
		return renderRichText(value.Text)
	case *tg.PageListItemBlocks:
		return renderPageBlocks(value.Blocks)
	}
	return ""
}

func renderOrderedList(items []tg.PageListOrderedItemClass) string {
	lines := make([]string, 0, len(items))
	for index, item := range items {
		number, text := renderOrderedListItem(item, index+1)
		if text != "" {
			prefix := number + ". "
			lines = append(lines, prefix+indentContinuation(text, strings.Repeat(" ", len(prefix))))
		}
	}
	return strings.Join(lines, "\n")
}

func renderOrderedListItem(item tg.PageListOrderedItemClass, fallbackNumber int) (string, string) {
	number := fmt.Sprintf("%d", fallbackNumber)
	switch value := item.(type) {
	case *tg.PageListOrderedItemText:
		if value.Num != "" {
			number = value.Num
		}
		return number, renderRichText(value.Text)
	case *tg.PageListOrderedItemBlocks:
		if value.Num != "" {
			number = value.Num
		}
		return number, renderPageBlocks(value.Blocks)
	default:
		return number, ""
	}
}

func renderTable(table *tg.PageBlockTable) string {
	if table == nil {
		return ""
	}

	lines := make([]string, 0, len(table.Rows)+1)
	if title := strings.TrimSpace(renderRichText(table.Title)); title != "" {
		lines = append(lines, title)
	}
	for _, row := range table.Rows {
		cells := make([]string, 0, len(row.Cells))
		for _, cell := range row.Cells {
			cells = append(cells, strings.TrimSpace(renderRichText(cell.Text)))
		}
		lines = append(lines, "| "+strings.Join(cells, " | ")+" |")
	}
	return strings.Join(lines, "\n")
}

func indentContinuation(text, indent string) string {
	return strings.ReplaceAll(text, "\n", "\n"+indent)
}
