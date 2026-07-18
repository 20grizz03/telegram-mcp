#!/usr/bin/env bash
# Daily digest built from a Telegram watchlist and delivered to a Telegram chat.
#
# Design: ALL Telegram I/O is deterministic via the `tgmcp` CLI (sync/dump/send).
# An LLM (Claude by default) is used ONLY as a text summarizer with the data piped
# in on stdin, so it never loads any MCP server and won't clash with a running
# `tgmcp serve` session. The set of chats/topics comes from the watchlist
# (`tgmcp watch add ...`), so adding a chat needs no edit here.
#
# Configure secrets + destination in "$TGMCP_HOME/env" (chmod 600):
#   export TG_APP_ID=...        # from https://my.telegram.org
#   export TG_APP_HASH=...
#   export DEST_CHAT=<chat id>  # from `tgmcp check` / list_chats
# Optional: DIGEST_PROMPT_FILE (custom summarizer prompt), WINDOW, SLOT_HOUR,
#           CLAUDE_BIN, TG_BIN.
#
# Run manually:  scripts/daily-digest.sh
# Or schedule it: see scripts/com.telegram-mcp.digest.plist (macOS launchd).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

# --- config (override via env or "$TGMCP_HOME/env") ---
export TGMCP_HOME="${TGMCP_HOME:-$HOME/.config/tg-mcp}"
TG_BIN="${TG_BIN:-$SCRIPT_DIR/../bin/tgmcp}"     # built with `make build` / `go build`
CLAUDE_BIN="${CLAUDE_BIN:-claude}"
WINDOW="${WINDOW:-24h}"
LOG="${LOG:-$TGMCP_HOME/digest.log}"
DIGEST_PROMPT_FILE="${DIGEST_PROMPT_FILE:-$SCRIPT_DIR/digest-prompt.default.txt}"
# UTF-8 locale so ${#str} counts characters (not bytes) for message chunking.
export LANG="${LANG:-en_US.UTF-8}" LC_ALL="${LC_ALL:-en_US.UTF-8}"

[ -f "$TGMCP_HOME/env" ] && . "$TGMCP_HOME/env"

: "${TG_APP_ID:?set TG_APP_ID (in $TGMCP_HOME/env)}"
: "${TG_APP_HASH:?set TG_APP_HASH (in $TGMCP_HOME/env)}"
: "${DEST_CHAT:?set DEST_CHAT to the destination chat id (in $TGMCP_HOME/env)}"
export TG_APP_ID TG_APP_HASH TGMCP_HOME

log() { printf '%s %s\n' "$(date '+%F %T')" "$*" >>"$LOG"; }

# run a command with a timeout (macOS lacks `timeout`). A background watchdog
# SIGKILLs it if it overruns; SIGKILL can't be caught/ignored, so even a Go binary
# stuck on a flaky network connection is stopped instead of hanging for hours.
run_to() {
  local t="$1"; shift
  "$@" <&0 &   # <&0 keeps the caller's stdin (bash /dev/null's a bg job's stdin otherwise)
  local pid=$!
  { sleep "$t"; kill -KILL "$pid"; } >/dev/null 2>&1 &
  local wd=$!
  wait "$pid" 2>/dev/null
  local rc=$?
  kill "$wd" 2>/dev/null
  return "$rc"
}

# send one message as HTML; fall back to plain text on malformed HTML.
send_msg() {
  run_to 60 "$TG_BIN" send --html "$DEST_CHAT" "$1" >>"$LOG" 2>&1 && return 0
  run_to 60 "$TG_BIN" send "$DEST_CHAT" "$1" >>"$LOG" 2>&1
}

# split text on line boundaries into <=limit-char chunks (never splits a line, so
# HTML tags stay intact) and send each — Telegram caps a message at 4096 chars.
send_chunked() {
  local limit=3500 buf="" line cand
  while IFS= read -r line || [ -n "$line" ]; do
    if [ -z "$buf" ]; then cand="$line"; else cand="$buf"$'\n'"$line"; fi
    if [ "${#cand}" -gt "$limit" ] && [ -n "$buf" ]; then
      send_msg "$buf" || return 1
      buf="$line"
    else
      buf="$cand"
    fi
  done <<< "$1"
  [ -n "$buf" ] && { send_msg "$buf" || return 1; }
  return 0
}

RAW=""; PROMPT_FILE=""
LOCKDIR="$TGMCP_HOME/digest.lock"
cleanup() { [ -n "$RAW" ] && rm -f "$RAW"; [ -n "$PROMPT_FILE" ] && rm -f "$PROMPT_FILE"; rmdir "$LOCKDIR" 2>/dev/null || true; }

# --- single-run lock (macOS has no flock; atomic mkdir) ---
if ! mkdir "$LOCKDIR" 2>/dev/null; then
  log "another digest run in progress, exiting"; exit 0
fi
trap cleanup EXIT

# Once-per-slot guard: schedulers may trigger this several times a day (e.g. to
# retry a run that failed while the connection was down, or catch up a run missed
# while the machine was off). Send only one digest per day: skip if a digest was
# already sent at or after today's SLOT_HOUR. A send that happened *before* the
# slot (e.g. a manual test run at night) must not swallow the morning digest.
# The marker is written only after a successful send, so failed runs still retry.
SLOT_HOUR="${SLOT_HOUR:-9}"
MARKER="$TGMCP_HOME/last-sent-at"   # epoch seconds of the last successful send
SLOT_EPOCH="$(date -j -f '%Y-%m-%d %H:%M:%S' "$(date '+%F') $(printf '%02d' "$SLOT_HOUR"):00:00" '+%s')"
LAST_SENT="$(cat "$MARKER" 2>/dev/null || echo 0)"
case "$LAST_SENT" in '' | *[!0-9]*) LAST_SENT=0 ;; esac
if [ "$LAST_SENT" -ge "$SLOT_EPOCH" ]; then
  log "digest for today's ${SLOT_HOUR}:00 slot already sent; skip"
  exit 0
fi

RAW="$(mktemp)"
log "digest start (window=$WINDOW)"

# Preflight: bail fast if Telegram is unreachable instead of grinding through
# per-topic timeouts. The marker stays unset, so a later trigger retries.
if ! run_to 25 "$TG_BIN" check >/dev/null 2>>"$LOG"; then
  log "telegram unreachable (preflight failed); will retry on next trigger"
  exit 1
fi

# --- collect: iterate the enabled watchlist; one labelled block per chat ---
# `watch list --plain` => "chatID \t topicsCSV \t label \t focus"
while IFS=$'\t' read -r CHAT TOPICS LABEL FOCUS; do
  [ -n "$CHAT" ] || continue
  if [ -n "$TOPICS" ]; then IFS=',' read -ra TS <<<"$TOPICS"; else TS=(0); fi
  CHATRAW="$(mktemp)"
  for T in "${TS[@]}"; do
    run_to 60 "$TG_BIN" sync "$CHAT" "$T"           >>"$LOG"     2>&1 || log "sync failed/timeout chat=$CHAT topic=$T"
    run_to 90 "$TG_BIN" dump "$CHAT" "$WINDOW" "$T" >>"$CHATRAW" 2>>"$LOG" || log "dump failed/timeout chat=$CHAT topic=$T"
  done
  if grep -q '^\[' "$CHATRAW"; then
    {
      echo "=== CHAT: ${LABEL:-$CHAT} ==="
      echo "FOCUS: ${FOCUS:-anything useful}"
      cat "$CHATRAW"
      echo
    } >>"$RAW"
  fi
  rm -f "$CHATRAW"
done < <("$TG_BIN" watch list --plain)

# Nothing collected (no "[date ...]" message lines) -> skip.
if ! grep -q '^\[' "$RAW"; then
  log "no messages in window; skipping send"; exit 0
fi

# --- summarize with the LLM (no tools; prompt + data piped on stdin) ---
# The instruction block comes from DIGEST_PROMPT_FILE (a custom prompt) or the
# bundled default; "%WINDOW%" in it is replaced with the actual window.
PREFS="$("$TG_BIN" pref list 2>/dev/null || true)"
PROMPT_FILE="$(mktemp)"
{
  sed "s/%WINDOW%/${WINDOW}/g" "$DIGEST_PROMPT_FILE"
  echo
  echo "PREFERENCES:"
  echo "$PREFS"
  echo
  echo "RAW MESSAGES BY CHAT:"
  cat "$RAW"
} >"$PROMPT_FILE"

# Run the summarizer with NO MCP servers: it only needs the piped-in text. This
# keeps it fast and avoids loading unrelated MCP servers configured for the CLI.
DIGEST="$(cd "$TGMCP_HOME" && run_to 420 "$CLAUDE_BIN" -p --strict-mcp-config --mcp-config '{"mcpServers":{}}' <"$PROMPT_FILE" 2>>"$LOG")" || { log "summarize failed/timeout"; exit 1; }
[ -n "$DIGEST" ] || { log "empty digest from summarizer"; exit 0; }

# --- deliver: one message per top-level section, split on the "§§§" markers the
# summarizer emits. The title rides on the first message; a section that alone
# exceeds Telegram's 4096-char limit is sub-chunked by send_chunked.
TITLE="${DIGEST_TITLE:-📊 Digest — $(date '+%F')}"
ok=1
first=1
section=""
flush() {
  local body
  body="$(printf '%s' "$1" | sed -e '/./,$!d')" # drop leading blank lines
  [ -n "$body" ] || return 0
  if [ "$first" = 1 ]; then body="$TITLE"$'\n\n'"$body"; first=0; fi
  send_chunked "$body"
}
while IFS= read -r line || [ -n "$line" ]; do
  if [ "$line" = "§§§" ]; then
    flush "$section" || ok=0
    section=""
  elif [ -z "$section" ]; then
    section="$line"
  else
    section="$section"$'\n'"$line"
  fi
done <<< "$DIGEST"
flush "$section" || ok=0

if [ "$ok" = 1 ]; then
  log "sent digest to $DEST_CHAT"
else
  log "send failed"; exit 1
fi
date '+%s' > "$MARKER"   # stamp the send so later same-day triggers skip this slot
log "digest done"
