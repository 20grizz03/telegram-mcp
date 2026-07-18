# telegram-mcp

MCP-сервер на Go для работы с Telegram через пользовательский аккаунт (MTProto,
[`gotd/td`](https://github.com/gotd/td)). Сервер отдаёт MCP-клиенту чаты, сообщения,
медиа и локальную аналитику, а при явном включении write-режима умеет публиковать
и редактировать посты.

Суммаризацию выполняет MCP-клиент: `telegram-mcp` отвечает только за получение и
изменение данных Telegram.

Изначальная версия проекта создана [@linemk](https://github.com/linemk). Инструменты
`send_message`, watchlist и готовый скрипт ежедневного дайджеста (`scripts/`) добавлены
им же в рамках совместной разработки.

> [!WARNING]
> Авторизация пользовательским аккаунтом через сторонний MTProto-клиент может
> противоречить правилам Telegram. Используйте отдельный аккаунт и собственные
> `api_id`/`api_hash`. Риск блокировки нельзя исключить.

## Возможности

### Чтение и локальная аналитика

| Tool | Назначение |
| --- | --- |
| `list_chats` | Список диалогов с типом, username и числом непрочитанных сообщений |
| `list_topics` | Топики форум-супергруппы |
| `read_chat` | История за период с авторами, метриками и ссылками на сообщения |
| `get_channel_profile` | Описание, число подписчиков и связанный чат обсуждения |
| `list_pinned_messages` | Закреплённые сообщения |
| `list_scheduled_posts` | Очередь отложенных постов |
| `list_invite_links` | Invite links и их статистика |
| `list_channel_folders` | Пользовательские папки Telegram |
| `sync_chat` | Инкрементальная синхронизация в локальный SQLite |
| `search_chat` | Полнотекстовый поиск по локальному кэшу |
| `download_media` | Скачивание вложения конкретного сообщения |
| `capture_growth_snapshot` | Локальный снимок аудитории и медианных метрик постов |
| `list_growth_snapshots` | История снимков роста и дельта |
| `save_preference`, `list_preferences`, `delete_preference` | Локальные правила для будущих summary |
| `save_partner`, `list_partners` | Локальная база каналов-партнёров |
| `create_outreach_draft`, `list_outreach_drafts`, `update_outreach_draft` | Локальные черновики outreach |
| `watch_add`, `watch_list`, `watch_remove` | Локальный watchlist чатов/топиков для ежедневного дайджеста, с per-chat `focus` (что извлекать/пропускать) |

CRM, preferences, snapshots, watchlist и outreach drafts хранятся только в SQLite
внутри `TGMCP_HOME`. Эти инструменты не отправляют сообщения в Telegram.

### Запись

Write-инструменты выключены по умолчанию. Они появляются только при
`TGMCP_ENABLE_WRITE=1` и выполняют реальные действия от имени авторизованного
аккаунта.

| Tool | Назначение |
| --- | --- |
| `publish_post` | Публикация текста или фото, сразу или по расписанию |
| `edit_post` | Изменение текста опубликованного поста |
| `pin_post` | Закрепление или открепление поста |
| `forward_post` | Нативная пересылка сообщения в другой чат |
| `create_invite_link` | Создание маркированной администраторской ссылки |
| `create_shared_folder` | Создание и экспорт папки с каналами/группами |
| `update_chat_description` | Изменение или очистка описания группы, супергруппы или канала |
| `update_profile_bio` | Изменение или очистка bio текущего Telegram-профиля |
| `send_message` | Отправка текстового или Telegram-HTML сообщения в чат/канал (напр. доставка дайджеста); `topic_id` для форум-топика, `html` для `<b>/<i>/<a>` |

## Требования

- macOS или Linux;
- Go 1.25+;
- Telegram `api_id` и `api_hash` из [my.telegram.org](https://my.telegram.org);
- MCP-клиент: Codex, Claude Desktop или другой клиент с поддержкой stdio MCP.

## Быстрая установка

```sh
git clone https://github.com/20grizz03/telegram-mcp.git
cd telegram-mcp
chmod +x install.sh
./install.sh
```

Установщик:

1. запросит ваши Telegram credentials;
2. соберёт бинарник локально;
3. проведёт одноразовый Telegram login;
4. создаст защищённые `mcp.env` и `run-server.sh` в `TGMCP_HOME`;
5. предложит подключить сервер к Codex и/или Claude Desktop.

По умолчанию данные хранятся в `~/.config/tg-mcp`. Полностью перезапустите
MCP-клиент после установки.

## Ручная установка

### 1. Сборка

```sh
go build -o bin/tgmcp ./cmd/tgmcp
```

### 2. Одноразовый login

```sh
export TG_APP_ID=123456
export TG_APP_HASH=xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
# Необязательно: export TG_PHONE=+1234567890

./bin/tgmcp login
```

Код подтверждения придёт в Telegram. Если включена 2FA, сервер также запросит
пароль. Сессия будет сохранена в `~/.config/tg-mcp/session.json`.

### 3. Запуск MCP-сервера

```sh
export TG_APP_ID=123456
export TG_APP_HASH=xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
./bin/tgmcp serve
```

Для безопасного постоянного подключения рекомендуется использовать
`run-server.sh`, который создаёт `install.sh`, а не хранить credentials прямо в
конфигурации MCP-клиента.

### Codex

```sh
codex mcp add telegram -- "$HOME/.config/tg-mcp/run-server.sh"
codex mcp list
```

### Claude Desktop

Добавьте runner в `mcpServers` файла `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "telegram": {
      "command": "/absolute/path/to/.config/tg-mcp/run-server.sh"
    }
  }
}
```

## Конфигурация

| Переменная | Обязательна | Назначение |
| --- | --- | --- |
| `TG_APP_ID` | да | Telegram `api_id` |
| `TG_APP_HASH` | да | Telegram `api_hash` |
| `TGMCP_HOME` | нет | Каталог с session и SQLite; по умолчанию `~/.config/tg-mcp` |
| `TG_PHONE` | нет | Номер телефона для login |
| `TG_PASSWORD` | нет | 2FA-пароль для login |
| `TGMCP_ENABLE_WRITE` | нет | `1`, `true` или `yes` включает реальные write-tools |

Диапазоны времени в `read_chat`: `today`, `yesterday`, `7d`, `24h`, `30m`, дата
`2026-06-15` или RFC3339. Для будущих `scheduled_at` и `expires_at` доступны
`7d`, `24h`, `30m`, дата или RFC3339.

## Примеры запросов

- «Покажи список моих чатов»
- «Саммаризируй чат `1234567890` за последние 7 дней»
- «Скачай фото из сообщения `50000` в чате `1234567890`»
- «Добавь Example Channel в кандидаты на взаимный репост»
- «Опубликуй этот текст завтра в 10:00 без уведомления» — только в write-режиме

## Ежедневный дайджест

`scripts/daily-digest.sh` собирает один дайджест в день из **watchlist** чатов и
доставляет его в выбранный Telegram-чат. Весь Telegram-I/O идёт через
детерминированный CLI (`sync`/`dump`/`send`), а LLM используется только как
суммаризатор текста на stdin (без MCP), поэтому джоба не конфликтует с запущенным
`tgmcp serve`. Набор чатов задаётся через `tgmcp watch add ...`, поэтому добавление
чата не требует правок скрипта. Подробнее — в [`scripts/README.md`](scripts/README.md).

Дополнительные CLI-команды помимо `login/serve`:

```text
tgmcp send [--html] <chat_id> "<text>" [topic_id]   # отправить сообщение
tgmcp dump <chat_id> [from] [topic_id]              # полный текст окна (вход дайджеста)
tgmcp watch add <chat_id> [topics_csv] [--label ..] [--focus ..] | watch list [--plain] | watch rm <id>
```

## Безопасность и приватность

- Никогда не коммитьте `api_hash`, телефон, 2FA-пароль или Telegram session.
- `session.json`, `mcp.env`, `.env`, SQLite-файлы, бинарники и каталоги данных
  исключены через `.gitignore`.
- Write-tools отключены по умолчанию и требуют явного `TGMCP_ENABLE_WRITE=1`.
- Для read-only использования не добавляйте write-флаг в окружение сервера.
- Перед публикацией форка или архива повторно проверьте его secret scanner'ом.

## Разработка

```sh
go test ./...
go vet ./...
```

Структура проекта:

```text
cmd/tgmcp/       CLI: login, serve, send, dump, watch и локальные команды
internal/config  env-конфигурация и пути данных
internal/tgclient MTProto-клиент и Telegram operations (вкл. send.go)
internal/store   SQLite cache, preferences, CRM, snapshots и watchlist
internal/syncer  синхронизация истории
internal/mcpserver MCP tools и handlers
scripts/         ежедневный дайджест (daily-digest.sh) и launchd-агент
```
