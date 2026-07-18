# Daily digest

`daily-digest.sh` builds one digest per day from a **watchlist** of Telegram
chats and delivers it to a Telegram chat of your choice. All Telegram I/O goes
through the deterministic `tgmcp` CLI; an LLM (Claude by default) is used only to
summarize the collected text piped in on stdin.

## How it works

1. Reads the enabled watchlist (`tgmcp watch list --plain`).
2. For each chat/topic: `tgmcp sync` (incremental cache) + `tgmcp dump` (full text
   for the window) into one labelled block with its `FOCUS`.
3. Pipes the blocks + your preferences into the summarizer using the prompt from
   `DIGEST_PROMPT_FILE` (default: `digest-prompt.default.txt`).
4. Splits the result on `§§§` markers and sends it via `tgmcp send --html`
   (falling back to plain text, chunked under Telegram's 4096-char limit).

Robustness: every Telegram call has a SIGKILL timeout (no multi-hour hangs on a
flaky connection); a preflight check bails fast when Telegram is unreachable; a
once-per-slot guard sends at most one digest per day even across retries.

## Setup

1. Build the binary: `make build` (or `go build -o bin/tgmcp ./cmd/tgmcp`) and log
   in once: `bin/tgmcp login`.
2. Create `~/.config/tg-mcp/env` (chmod 600):
   ```sh
   export TG_APP_ID=...        # from https://my.telegram.org
   export TG_APP_HASH=...
   export DEST_CHAT=<chat id>  # target chat, from `tgmcp check` / list_chats
   ```
3. Build your watchlist (each chat gets a `--focus` brief):
   ```sh
   bin/tgmcp watch add <chat_id> <topic_ids_csv> --label "My chat" --focus "what to extract; skip X"
   bin/tgmcp watch list
   ```
4. Run it: `scripts/daily-digest.sh` (or `WINDOW=48h scripts/daily-digest.sh` for
   a wider window while testing).

## Scheduling (macOS)

Edit `com.telegram-mcp.digest.plist` (replace the `__PLACEHOLDERS__`), then:

```sh
cp scripts/com.telegram-mcp.digest.plist ~/Library/LaunchAgents/
launchctl load -w ~/Library/LaunchAgents/com.telegram-mcp.digest.plist
```

## Environment variables

| Var | Default | Meaning |
| --- | --- | --- |
| `TGMCP_HOME` | `~/.config/tg-mcp` | session, cache DB, `env`, logs, run marker |
| `TG_BIN` | `<repo>/bin/tgmcp` | path to the built binary |
| `CLAUDE_BIN` | `claude` | summarizer CLI (reads the prompt on stdin) |
| `WINDOW` | `24h` | how far back to collect (`24h`, `48h`, `7d`, ...) |
| `SLOT_HOUR` | `9` | the daily slot; sends at most one digest per day at/after it |
| `DIGEST_PROMPT_FILE` | `digest-prompt.default.txt` | summarizer instructions (`%WINDOW%` is substituted) |
| `DIGEST_TITLE` | `📊 Digest — <date>` | first-message title |

Secrets and `DEST_CHAT` live in `$TGMCP_HOME/env` and are never committed.
