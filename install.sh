#!/usr/bin/env bash

set -euo pipefail
umask 077

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN_PATH="$ROOT_DIR/bin/tgmcp"

usage() {
  cat <<'EOF'
telegram-mcp v0.4.3 installer

Usage: ./install.sh

The installer asks for your own Telegram api_id/api_hash, performs the one-time
Telegram login, and connects the local MCP server to Codex and/or Claude Desktop.
It does not send credentials anywhere except Telegram's official API during login.
EOF
}

if [[ "${1:-}" == "--help" || "${1:-}" == "-h" ]]; then
  usage
  exit 0
fi
if [[ $# -ne 0 ]]; then
  usage >&2
  exit 2
fi

printf '\ntelegram-mcp v0.4.3 — интерактивная установка\n\n'
printf '%s\n' 'Сначала получите собственные Telegram credentials:'
printf '%s\n' '  1. Откройте https://my.telegram.org'
printf '%s\n' '  2. Войдите по номеру телефона'
printf '%s\n' '  3. Откройте API development tools'
printf '%s\n' '  4. Создайте приложение с Platform = Desktop'
printf '%s\n\n' '  5. Скопируйте App api_id и App api_hash'
printf '%s\n' 'api_hash не будет показан при вводе.'

while true; do
  printf 'App api_id (только цифры): '
  IFS= read -r TG_APP_ID
  if [[ "$TG_APP_ID" =~ ^[0-9]+$ ]]; then
    break
  fi
  printf '%s\n' 'Некорректный api_id: ожидается число.' >&2
done

while true; do
  printf 'App api_hash (32 hex-символа): '
  IFS= read -r -s TG_APP_HASH
  printf '\n'
  if [[ "$TG_APP_HASH" =~ ^[0-9a-fA-F]{32}$ ]]; then
    break
  fi
  printf '%s\n' 'Некорректный api_hash: ожидаются 32 символа 0-9/a-f.' >&2
done

DEFAULT_HOME="$HOME/.config/tg-mcp"
printf 'Каталог данных [%s]: ' "$DEFAULT_HOME"
IFS= read -r TGMCP_HOME
TGMCP_HOME="${TGMCP_HOME:-$DEFAULT_HOME}"
if [[ "$TGMCP_HOME" == "~/"* ]]; then
  TGMCP_HOME="$HOME/${TGMCP_HOME:2}"
fi
mkdir -p "$TGMCP_HOME"
chmod 700 "$TGMCP_HOME"

SYSTEM="$(uname -s)"
ARCH="$(uname -m)"
if [[ "$SYSTEM" == "Darwin" && "$ARCH" == "arm64" && -f "$BIN_PATH" ]]; then
  chmod +x "$BIN_PATH"
  if command -v xattr >/dev/null 2>&1; then
    xattr -d com.apple.quarantine "$BIN_PATH" 2>/dev/null || true
  fi
  printf '%s\n' 'Использую готовый бинарник для macOS Apple Silicon.'
else
  if ! command -v go >/dev/null 2>&1; then
    printf '%s\n' "Готовый бинарник архива предназначен для macOS arm64, а обнаружено: $SYSTEM $ARCH." >&2
    printf '%s\n' 'Установите Go 1.25+ с https://go.dev/dl/ и снова запустите ./install.sh.' >&2
    exit 1
  fi
  printf '%s\n' 'Собираю бинарник для текущей системы...'
  mkdir -p "$ROOT_DIR/bin"
  (
    cd "$ROOT_DIR"
    go build -trimpath -ldflags '-s -w' -o "$BIN_PATH" ./cmd/tgmcp
  )
  chmod +x "$BIN_PATH"
fi

ENABLE_WRITE=0
printf 'Включить реальные публикации и другие write-команды? [y/N]: '
IFS= read -r WRITE_ANSWER
case "$WRITE_ANSWER" in
  y|Y|yes|YES|Yes|да|Да|ДА) ENABLE_WRITE=1 ;;
esac

printf '\n%s\n' 'Сейчас начнётся одноразовый Telegram-login.'
printf '%s\n' 'Код придёт в Telegram; 2FA-пароль понадобится только если он включён.'
TG_APP_ID="$TG_APP_ID" \
TG_APP_HASH="$TG_APP_HASH" \
TGMCP_HOME="$TGMCP_HOME" \
  "$BIN_PATH" login

ENV_FILE="$TGMCP_HOME/mcp.env"
RUNNER="$TGMCP_HOME/run-server.sh"
{
  printf 'TG_APP_ID=%q\n' "$TG_APP_ID"
  printf 'TG_APP_HASH=%q\n' "$TG_APP_HASH"
  printf 'TGMCP_HOME=%q\n' "$TGMCP_HOME"
  if [[ "$ENABLE_WRITE" -eq 1 ]]; then
    printf '%s\n' 'TGMCP_ENABLE_WRITE=1'
  fi
} > "$ENV_FILE"
chmod 600 "$ENV_FILE"

{
  printf '%s\n' '#!/usr/bin/env bash'
  printf '%s\n' 'set -euo pipefail'
  printf 'set -a; source %q; set +a\n' "$ENV_FILE"
  printf 'exec %q serve\n' "$BIN_PATH"
} > "$RUNNER"
chmod 700 "$RUNNER"

configure_codex() {
  if ! command -v codex >/dev/null 2>&1; then
    printf '\n%s\n' 'Команда codex не найдена. Добавьте в ~/.codex/config.toml:'
    printf '\n[mcp_servers.telegram]\ncommand = "%s"\n' "$RUNNER"
    return
  fi

  if codex mcp list 2>/dev/null | grep -Eq '(^|[[:space:]])telegram([[:space:]]|$)'; then
    printf 'MCP-сервер telegram уже зарегистрирован в Codex. Заменить? [y/N]: '
    IFS= read -r REPLACE_CODEX
    case "$REPLACE_CODEX" in
      y|Y|yes|YES|Yes|да|Да|ДА) codex mcp remove telegram ;;
      *) printf '%s\n' 'Конфигурация Codex оставлена без изменений.'; return ;;
    esac
  fi
  codex mcp add telegram -- "$RUNNER"
  printf '%s\n' 'Codex настроен. Проверка: codex mcp list'
}

configure_claude() {
  case "$SYSTEM" in
    Darwin) CLAUDE_CONFIG="$HOME/Library/Application Support/Claude/claude_desktop_config.json" ;;
    Linux) CLAUDE_CONFIG="$HOME/.config/Claude/claude_desktop_config.json" ;;
    *)
      printf '%s\n' 'Автонастройка Claude для этой ОС не поддерживается; см. SETUP.md.'
      return
      ;;
  esac

  if ! command -v python3 >/dev/null 2>&1; then
    printf '%s\n' 'Для безопасного обновления JSON-конфига Claude нужен python3.' >&2
    printf '%s\n' "Добавьте MCP вручную: command = $RUNNER" >&2
    return
  fi

  TGMCP_CLAUDE_CONFIG="$CLAUDE_CONFIG" TGMCP_RUNNER="$RUNNER" python3 - <<'PY'
import json
import os
import shutil
import tempfile
from datetime import datetime
from pathlib import Path

path = Path(os.environ["TGMCP_CLAUDE_CONFIG"])
runner = os.environ["TGMCP_RUNNER"]
path.parent.mkdir(parents=True, exist_ok=True)

if path.exists() and path.stat().st_size:
    with path.open("r", encoding="utf-8") as fh:
        data = json.load(fh)
    backup = path.with_name(path.name + ".backup-" + datetime.now().strftime("%Y%m%d-%H%M%S"))
    shutil.copy2(path, backup)
    print(f"Резервная копия Claude config: {backup}")
else:
    data = {}

servers = data.setdefault("mcpServers", {})
servers["telegram"] = {"command": runner}

fd, tmp_name = tempfile.mkstemp(prefix=path.name + ".", dir=path.parent)
try:
    with os.fdopen(fd, "w", encoding="utf-8") as fh:
        json.dump(data, fh, ensure_ascii=False, indent=2)
        fh.write("\n")
    os.chmod(tmp_name, 0o600)
    os.replace(tmp_name, path)
except Exception:
    try:
        os.unlink(tmp_name)
    except FileNotFoundError:
        pass
    raise

print(f"Claude Desktop настроен: {path}")
PY
}

printf '\nКуда подключить MCP?\n'
printf '%s\n' '  1) Codex'
printf '%s\n' '  2) Claude Desktop'
printf '%s\n' '  3) В оба клиента'
printf '%s\n' '  4) Пока не подключать'
printf 'Выбор [1]: '
IFS= read -r CLIENT_CHOICE
CLIENT_CHOICE="${CLIENT_CHOICE:-1}"
case "$CLIENT_CHOICE" in
  1) configure_codex ;;
  2) configure_claude ;;
  3) configure_codex; configure_claude ;;
  4) printf '%s\n' "Сервер можно подключить позже через runner: $RUNNER" ;;
  *) printf '%s\n' 'Неизвестный выбор; автоматическое подключение пропущено.' ;;
esac

unset TG_APP_HASH

printf '\n%s\n' 'Установка завершена.'
printf '%s\n' "Данные и сессия: $TGMCP_HOME"
printf '%s\n' "Защищённый файл credentials: $ENV_FILE"
printf '%s\n' 'Полностью перезапустите Codex/Claude Desktop перед использованием.'
