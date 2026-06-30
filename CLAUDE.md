# sudoku-table-bot — CLAUDE.md

Telegram-бот для учёта игр лиги в судоку (usdoku.com): очки, места, сезоны, дуэли.
Маленький, один инстанс, один писатель в БД. Группа из 3 друзей.

## Стек
- **Go 1.25**, `gopkg.in/telebot.v3` (long-polling, ParseMode HTML).
- **modernc.org/sqlite** — pure-Go SQLite, **без CGO** → статичный бинарь на alpine.
- SQLite WAL, `SetMaxOpenConns(1)` (один писатель).
- ARQ/Redis нет. Внешний API: usdoku (`internal/usdoku`, недокументированный HTTP).

## Структура
- `cmd/bot/main.go` — энтрипоинт.
- `internal/bot/` — хендлеры + рендер сообщений (telebot). Файлы по доменам:
  `handlers_game.go` (newgame/result/picker/finalize), `handlers_duel.go`,
  `handlers_invite.go`, `handlers_auto.go` (watch usdoku + auto-record),
  `handlers_stats.go`, `handlers_basic.go`, `handlers_admin.go`, `reminders.go`,
  `keyboards.go`, `messages.go` (все текст-билдеры), `bot.go` (routes, helpers).
- `internal/storage/` — слой БД (database/sql, сырой SQL). `store.go` (Open+migrate),
  `schema.sql` (embed, применяется на каждом Open), `games.go`, `players.go`,
  `seasons.go`, `standings.go`, `stats.go`, `duels.go`.
- `internal/domain/scoring.go` — чистая логика очков (`PointsForRank`).
- `data/sudoku.db` — прод БД (volume в docker).

## Сборка / деплой (важно)
- **На хосте нет `go`** → `just build/test/vet/run` падают (`go: command not found`).
  Любой go-тулинг гоняй через docker:
  ```
  docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go build ./... && go vet ./... && go test ./...'
  ```
- Деплой: `docker compose up -d --build --force-recreate`.
  **`just up` / `docker compose up -d --build` без `--force-recreate` НЕ пересоздаёт
  контейнер** — оставляет старый бинарь. Всегда `--force-recreate`.
- После деплоя проверяй `docker compose logs bot | tail` → `bot started`.
- Прод-БД операции: `docker run --rm -v "$PWD/data":/data ... golang:1.25-alpine`
  с `apk add sqlite`. SQL-литералы — **одинарные кавычки** (двойные SQLite читает
  как идентификаторы). Пиши SQL в файл и монтируй (`-v /tmp/x.sql:/q.sql`), не heredoc
  с кавычками. Перед мутацией — `sqlite3 db ".backup /data/db.bak-..."` (WAL-safe).

## Модель данных (неочевидное)
- `games.duel_target_id`: **NULL = сезонная игра, set = дуэль**. Дуэли **исключены из
  сезона** — фильтр `duel_target_id IS NULL` стоит на ВСЕХ сезонных запросах
  (`Standings`, `SpeedFor`, `Speedboard`, `ExportGames`, `RecentGames`). НЕ на
  `CompletedToday` (дуэль = «играл сегодня»).
- `games.deleted`: soft-delete. `ActivePendingGame`/`PendingGamesWithCode` фильтруют
  `deleted=0`; `watchGame` останавливается на `g.Deleted` (иначе отменённая дуэль
  могла бы авто-записаться).
- `game_results.rank`: 1-based место финиша. **`rank = 0` = DNF** (зашёл, не доиграл).
  `PointsForRank(table, 0) = 0` (rank<1 → 0), так что DNF скорится 0 без миграций.
  - `GameResults` сортирует `ORDER BY (gr.rank=0), gr.rank` (DNF в конец).
  - `finishOrder` (/history) фильтрует `rank > 0`.
  - `Standings`: wins = `rank=1`, games = `COUNT(gr.id)` (DNF считается сыгранной).
  - Дуэль: **DNF = поражение** → losses = `rank<>1` (DuelRecord/Leaderboard/RecentDuels).
  - Авто-запись (`autoRecord`) пишет финишеров (`AddPickTimed`), затем joined-но-DNF
    (`AddDNF`, rank 0). Ручной `/result` рангов-0 не создаёт.
- `game_results.duration_secs`: время решения, **только auto-record** (usdoku
  joinedAt/completedAt); ручные = NULL. Speed-статы фильтруют `IS NOT NULL`.
- `seasons`: **один active на чат** — partial unique index `idx_one_active_season ON
  seasons(chat_id) WHERE status='active'`. Индекс создаётся в `migrate()` ПОСЛЕ
  self-healing дедупа (оставить active-сезон с макс. числом игр), т.к. `schema.sql`
  бежит ДО `migrate()`. `ActiveSeason`/`CloseSeason` используют
  `INSERT ... ON CONFLICT(chat_id) WHERE status='active' DO NOTHING` + re-read
  (read-then-create гонка плодила дубли). Season «номер» (`number`) ≠ row id.
- `players`: `tg_id` (есть у всех через /join), `username` (часто пусто — не у всех
  есть @ник в Telegram), `usdoku_nick` (для авто-матчинга), `active` (soft-remove).
- `game_rsvp`: ростер «Я в деле» для /invite. `ToggleRsvp` = INSERT OR IGNORE / DELETE.
- **Рейтинг (ELO)**: чистая функция истории — `domain.ComputeRatings` прогоняет
  ВСЕ завершённые игры (`sqlCompletedGames`, сезон + дуэли), без таблицы и без
  инкрементального стейта (`storage.GamesForRating` — вход). Вечный (не сбрасывается
  по сезонам), параллелен очкам сезона. Чистый ранк, многопользовательская =
  попарно (K делится на число оппонентов), DNF = поражение финишёрам (DNF-vs-DNF
  игнор). Старт 1000; provisional <10 игр K=64, иначе K=32. Корона = текущий №1
  (при равенстве держит incumbent). Дельта пишется в пост результата
  (`ratingFooter`), полная лестница — `/rating`. Edit/delete игры → следующий
  replay сам пересчитает.

## Конвенции
- **Conventional Commits**, БЕЗ `Co-Authored-By` трейлера (стиль репо).
- HTML parse mode: **всё пользовательское — через `esc()`**; пинг игрока — `mention()`
  (tg://user?id= ссылка → @username → имя). `@username` пингует надёжнее, но у многих
  его нет → tg-ссылка (уведомляет участников, но рендерится как текст у тех, кого
  клиент не подтянул).
- **Эфемерные сообщения**: служебный шум (ошибки, гарды, валидация, `fail()`) шлётся
  через `b.ephemeral()` → само-удаляется через 20с + удаляет команду юзера (best-effort,
  нужен бот-админ). Ценное (результаты/статы/игровые посты с кнопками/подтверждения/
  напоминания) НЕ эфемерно. Callback-тосты (`c.Respond`) уже эфемерны.
- Пикеры/подтверждения — через `c.Edit` (трансформируются, не плодят сообщения).
- Тесты — TDD, гоняются в docker. Чистые функции (рендер, scoring, SQL-методы)
  покрыты; Telegram-I/O glue — нет.

## Гочи
- Дуэль создаёт игру с `duel_target_id`; отмена дуэли = `SoftDeleteGame` (освобождает
  слот pending-игры; `watchGame` стопается).
- `createGameRoom` — общий core для /newgame, /duel, /invite (guards + usdoku-комната
  + watcher), НЕ постит сообщение (caller рендерит своё). `(nil, nil)` = guard уже
  ответил.
- usdoku-таймстампы: единица не подтверждена; `Player.SolveSeconds()` авто-нормализует
  (>6ч → делит на 1000, считая мс).
- Множественные active-сезона уже были в проде (read-then-create гонка) — починено
  индексом + дедупом; `/restore` старого бэкапа лечится self-healing миграцией.
