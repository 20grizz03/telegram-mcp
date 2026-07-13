// Command tgmcp is a Telegram reader exposed to Claude Desktop as an MCP server.
//
// Usage:
//
//	tgmcp login   # one-time interactive auth in a terminal -> creates session
//	tgmcp serve   # run the MCP server over stdio (for Claude Desktop)
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/20grizz03/telegram-mcp/internal/config"
	"github.com/20grizz03/telegram-mcp/internal/mcpserver"
	"github.com/20grizz03/telegram-mcp/internal/store"
	"github.com/20grizz03/telegram-mcp/internal/syncer"
	"github.com/20grizz03/telegram-mcp/internal/tgclient"
)

// linkFor rebuilds a clickable t.me link for a search hit using cached chat meta.
func linkFor(ctx context.Context, st *store.Store, h store.SearchHit) string {
	meta, ok, _ := st.GetChat(ctx, h.ChatID)
	if !ok {
		return ""
	}
	return tgclient.BuildLink(tgclient.LinkArgs{
		Kind:      tgclient.PeerKind(meta.Kind),
		ChannelID: h.ChatID, // exposed id == raw channel id for channels
		Username:  meta.Username,
		MsgID:     h.MsgID,
		TopicID:   h.TopicID,
	})
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	if err := run(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(cmd string) error {
	// pref manages learned rules directly in the DB and needs no Telegram creds.
	if cmd == "pref" {
		return runPref(os.Args[2:])
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	tc, err := tgclient.New(cfg)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch cmd {
	case "login":
		return tc.Run(ctx, func(ctx context.Context) error {
			return tc.Login(ctx)
		})

	case "check":
		return tc.Run(ctx, func(ctx context.Context) error {
			if err := tc.EnsureAuthorized(ctx); err != nil {
				if errors.Is(err, tgclient.ErrNotAuthorized) {
					fmt.Fprintln(os.Stderr, "no session — run `tgmcp login` first")
				}
				return err
			}
			self, err := tc.Self(ctx)
			if err != nil {
				return err
			}
			fmt.Printf("Logged in as %s %s (@%s) id=%d\n",
				self.FirstName, self.LastName, self.Username, self.ID)

			chats, err := tc.ListChats(ctx, "", 15)
			if err != nil {
				return err
			}
			fmt.Printf("\nTop %d chats:\n", len(chats))
			for _, ch := range chats {
				fmt.Printf("  id=%-14d %-8s forum=%-5v unread=%-4d %s\n",
					ch.ID, ch.Kind, ch.IsForum, ch.Unread, ch.Title)
			}
			return nil
		})

	case "topics":
		if len(os.Args) < 3 {
			return fmt.Errorf("usage: tgmcp topics <chat_id>")
		}
		id, err := strconv.ParseInt(os.Args[2], 10, 64)
		if err != nil {
			return fmt.Errorf("bad chat_id: %w", err)
		}
		return tc.Run(ctx, func(ctx context.Context) error {
			if err := tc.EnsureAuthorized(ctx); err != nil {
				return err
			}
			topics, err := tc.ListTopics(ctx, id)
			if err != nil {
				return err
			}
			fmt.Printf("%d topics:\n", len(topics))
			for _, t := range topics {
				fmt.Printf("  topic_id=%-8d top_msg=%-8d %s\n", t.ID, t.TopMessageID, t.Title)
			}
			return nil
		})

	case "read":
		if len(os.Args) < 3 {
			return fmt.Errorf("usage: tgmcp read <chat_id> [from] [topic_id]")
		}
		id, err := strconv.ParseInt(os.Args[2], 10, 64)
		if err != nil {
			return fmt.Errorf("bad chat_id: %w", err)
		}
		from := "today"
		if len(os.Args) >= 4 {
			from = os.Args[3]
		}
		topicID := 0
		if len(os.Args) >= 5 {
			topicID, _ = strconv.Atoi(os.Args[4])
		}
		return tc.Run(ctx, func(ctx context.Context) error {
			if err := tc.EnsureAuthorized(ctx); err != nil {
				return err
			}
			chat, msgs, err := tc.ReadChat(ctx, id, topicID, from, "", 50)
			if err != nil {
				return err
			}
			fmt.Printf("%s — %d messages (from=%s topic=%d)\n", chat.Title, len(msgs), from, topicID)
			for _, m := range msgs {
				fmt.Printf("[%s %s] %s: %.80s  %s\n",
					m.DateISO[:10], m.TimeLocal, m.Author.Name, m.Text, m.Link)
			}
			return nil
		})

	case "sync":
		if len(os.Args) < 3 {
			return fmt.Errorf("usage: tgmcp sync <chat_id> [topic_id] [max]")
		}
		id, err := strconv.ParseInt(os.Args[2], 10, 64)
		if err != nil {
			return fmt.Errorf("bad chat_id: %w", err)
		}
		topicID := 0
		if len(os.Args) >= 4 {
			topicID, _ = strconv.Atoi(os.Args[3])
		}
		max := 0
		if len(os.Args) >= 5 {
			max, _ = strconv.Atoi(os.Args[4])
		}
		st, err := store.Open(cfg.DBPath())
		if err != nil {
			return err
		}
		defer st.Close()
		return tc.Run(ctx, func(ctx context.Context) error {
			if err := tc.EnsureAuthorized(ctx); err != nil {
				return err
			}
			res, err := syncer.Sync(ctx, tc, st, id, topicID, max)
			if err != nil {
				return err
			}
			fmt.Printf("synced %q: +%d new, %d cached total, last_msg_id=%d\n",
				res.Chat.Title, res.Fetched, res.TotalCached, res.LastMsgID)
			return nil
		})

	case "search":
		if len(os.Args) < 3 {
			return fmt.Errorf("usage: tgmcp search <query> [chat_id]")
		}
		query := os.Args[2]
		var chatID *int64
		if len(os.Args) >= 4 {
			id, err := strconv.ParseInt(os.Args[3], 10, 64)
			if err != nil {
				return fmt.Errorf("bad chat_id: %w", err)
			}
			chatID = &id
		}
		st, err := store.Open(cfg.DBPath())
		if err != nil {
			return err
		}
		defer st.Close()
		hits, err := st.SearchMessages(context.Background(), chatID, query, 30)
		if err != nil {
			return err
		}
		fmt.Printf("%d hits for %q:\n", len(hits), query)
		for _, h := range hits {
			link := linkFor(context.Background(), st, h)
			fmt.Printf("[%s] %s: %.80s  %s\n",
				time.Unix(h.Date, 0).Format("2006-01-02 15:04"), h.Sender, h.Text, link)
		}
		return nil

	case "serve":
		st, err := store.Open(cfg.DBPath())
		if err != nil {
			return err
		}
		defer st.Close()
		return tc.Run(ctx, func(ctx context.Context) error {
			if err := tc.EnsureAuthorized(ctx); err != nil {
				if errors.Is(err, tgclient.ErrNotAuthorized) {
					fmt.Fprintln(os.Stderr, "no session found — run `tgmcp login` in a terminal first")
				}
				return err
			}
			fmt.Fprintln(os.Stderr, "telegram-mcp ready (stdio)")
			return mcpserver.Build(tc, st, cfg.EnableWrite).Run(ctx, &mcp.StdioTransport{})
		})

	default:
		usage()
		return fmt.Errorf("unknown command %q", cmd)
	}
}

// runPref manages preference rules directly in the SQLite store.
//
//	tgmcp pref add "<rule>" [--chat <id>]
//	tgmcp pref list [--chat <id>]
//	tgmcp pref rm <id>
func runPref(args []string) error {
	home, err := config.ResolveHome()
	if err != nil {
		return err
	}
	st, err := store.Open(config.Config{Home: home}.DBPath())
	if err != nil {
		return err
	}
	defer st.Close()
	ctx := context.Background()

	if len(args) == 0 {
		return fmt.Errorf(`usage: tgmcp pref add "<rule>" [--chat <id>] | pref list [--chat <id>] | pref rm <id>`)
	}

	switch args[0] {
	case "add":
		var rule string
		var chatID *int64
		rest := args[1:]
		for i := 0; i < len(rest); i++ {
			if rest[i] == "--chat" && i+1 < len(rest) {
				id, err := strconv.ParseInt(rest[i+1], 10, 64)
				if err != nil {
					return fmt.Errorf("bad --chat: %w", err)
				}
				chatID = &id
				i++
			} else if rule == "" {
				rule = rest[i]
			}
		}
		if rule == "" {
			return fmt.Errorf(`usage: tgmcp pref add "<rule>" [--chat <id>]`)
		}
		p, err := st.AddPreference(ctx, rule, chatID)
		if err != nil {
			return err
		}
		fmt.Printf("saved #%d [%s] %s\n", p.ID, scopeLabel(p.Scope, p.ChatID), p.Rule)
		return nil

	case "list":
		var chatID *int64
		if len(args) >= 3 && args[1] == "--chat" {
			id, err := strconv.ParseInt(args[2], 10, 64)
			if err != nil {
				return fmt.Errorf("bad --chat: %w", err)
			}
			chatID = &id
		}
		prefs, err := st.ListPreferences(ctx, chatID)
		if err != nil {
			return err
		}
		if len(prefs) == 0 {
			fmt.Println("no preferences")
			return nil
		}
		for _, p := range prefs {
			fmt.Printf("#%-3d [%s] %s\n", p.ID, scopeLabel(p.Scope, p.ChatID), p.Rule)
		}
		return nil

	case "rm":
		if len(args) < 2 {
			return fmt.Errorf("usage: tgmcp pref rm <id>")
		}
		id, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil {
			return fmt.Errorf("bad id: %w", err)
		}
		ok, err := st.DeletePreference(ctx, id)
		if err != nil {
			return err
		}
		if ok {
			fmt.Printf("deleted #%d\n", id)
		} else {
			fmt.Printf("no preference #%d\n", id)
		}
		return nil

	default:
		return fmt.Errorf("unknown pref subcommand %q (add|list|rm)", args[0])
	}
}

func scopeLabel(scope string, chatID *int64) string {
	if chatID != nil {
		return fmt.Sprintf("chat:%d", *chatID)
	}
	return scope
}

func usage() {
	fmt.Fprint(os.Stderr, `tgmcp — Telegram summarizer MCP server

Commands:
  login   one-time interactive login (terminal), creates the session
  check   verify the saved session: print self + first chats
  pref    manage learned rules: pref add "<rule>" [--chat <id>] | pref list [--chat <id>] | pref rm <id>
  sync    cache a chat's messages: sync <chat_id> [topic_id] [max]
  search  full-text search the cache: search <query> [chat_id]
  serve   run the MCP server over stdio (configured in Claude Desktop)

Required env: TG_APP_ID, TG_APP_HASH   (from https://my.telegram.org)
Optional env: TGMCP_HOME (default ~/.config/tg-mcp), TG_PHONE, TG_PASSWORD, TGMCP_ENABLE_WRITE
`)
}
