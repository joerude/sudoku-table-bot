# sudoku-table-bot: CLAUDE.md

Telegram-бот для учёта игр лиги в судоку (usdoku.com): очки, места, сезоны, дуэли.
Маленький, один инстанс, один писатель в БД. Группа из 3 друзей.

## Стек
- **Go 1.25**, `gopkg.in/telebot.v3` (long-polling, ParseMode HTML).
- **telebot это форк**: `third_party/telebot.v3` (v3.3.8 + `replace` в go.mod), потому
  что ни один релиз (v3.4 beta, v4 beta) не знает Bot API 10.x. Только добавленные
  поля: эфемерные (`Message.EphemeralMessageID`, `ReceiverUser`,
  `Command.IsEphemeral`), `Style`/`Disabled` у кнопок. Список и как обновлять:
  `third_party/telebot.v3/PATCHES.md`. Dockerfile копирует `third_party` до
  `go mod download`. Эфемерные методы зовём через `tb.Raw` (`internal/bot/tgx.go`);
  клавиатуру в Raw прогоняй через `rawMarkup` (кодирует `\f<unique>|<data>`).
- **modernc.org/sqlite**: pure-Go SQLite, **без CGO** → статичный бинарь на alpine.
- SQLite WAL, `SetMaxOpenConns(1)` (один писатель).
- ARQ/Redis нет. Внешний API: usdoku (`internal/usdoku`, недокументированный HTTP).

## Структура
- `cmd/bot/main.go`: энтрипоинт.
- `internal/bot/`: хендлеры + рендер сообщений (telebot). Файлы по доменам:
  `handlers_game.go` (newgame/result/picker/finalize), `handlers_duel.go`,
  `handlers_invite.go`, `handlers_auto.go` (watch usdoku + auto-record),
  `handlers_stats.go`, `handlers_basic.go`, `handlers_admin.go`, `reminders.go`,
  `keyboards.go`, `messages.go` (все текст-билдеры), `bot.go` (routes, helpers).
- `internal/storage/`: слой БД (database/sql, сырой SQL). `store.go` (Open+migrate),
  `schema.sql` (embed, применяется на каждом Open), `games.go`, `players.go`,
  `seasons.go`, `standings.go`, `stats.go`, `duels.go`.
- `internal/domain/scoring.go`: чистая логика очков (`PointsForRank`).
- `internal/chart/`: PNG-картинки (бета): гонка сезона и календарь (кнопки под
  таблицей /stats), график Elo (вкладка «Рейтинг»), постер сезона (сам после поста
  о закрытии сезона + кнопка в сводке архивного сезона). Хендлеры в
  `internal/bot/charts.go`. Рендер только в памяти, на диск ничего не пишется.
  Чистый Go на `golang.org/x/image` (gofont, рисует в 2x и сжимает), без CGO.
  Посмотреть картинки: `CHART_DIR=/tmp/x go test ./internal/chart`.
  Держи `x/image` на версии с `go 1.25` в go.mod (v0.46+ требуют Go 1.26).
- `data/sudoku.db`: прод БД (volume в docker).

## Сборка / деплой (важно)
- **На хосте нет `go`** → `just build/test/vet/run` падают (`go: command not found`).
  Любой go-тулинг гоняй через docker:
  ```
  docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go build ./... && go vet ./... && go test ./...'
  ```
- Деплой: `docker compose up -d --build --force-recreate`.
  **`just up` / `docker compose up -d --build` без `--force-recreate` НЕ пересоздаёт
  контейнер**, оставляет старый бинарь. Всегда `--force-recreate`.
- **Build-кэш тоже залипает**: `--build` может взять несвежий кэш и передеплоить
  старый бинарь (образ не пересобрался, тот же image SHA). Если задеплоил, а нового
  кода нет: `docker compose build --no-cache bot`, потом `up -d --force-recreate`.
  Проверка нового бинаря: `docker compose exec -T bot sh -c 'strings /app/sudoku-bot
  | grep -a "<уникальная-строка>"'` (Go пакует литералы, ищи подстроку, не `^строка$`).
- После деплоя проверяй `docker compose logs bot | tail` → `bot started`.
- Прод-БД операции: `docker run --rm -v "$PWD/data":/data ... golang:1.25-alpine`
  с `apk add sqlite`. SQL-литералы в **одинарных кавычках** (двойные SQLite читает
  как идентификаторы). Пиши SQL в файл и монтируй (`-v /tmp/x.sql:/q.sql`), не heredoc
  с кавычками. Перед мутацией: `sqlite3 db ".backup /data/db.bak-..."` (WAL-safe).
- `OWNER_TG_ID` (env, опционально): владелец бота. `/backup` и `/restore` гейтит
  `requireDBAccess`: владелец где угодно, иначе только админ группы (в личке никто).

## Модель данных (неочевидное)
- `games.duel_target_id`: **NULL = сезонная игра, set = дуэль**. Дуэли **исключены из
  сезона**: фильтр `duel_target_id IS NULL` стоит на ВСЕХ сезонных запросах
  (`Standings`, `SpeedFor`, `Speedboard`, `ExportGames`, `RecentGames`,
  `SeasonGameCount`). НЕ на `CompletedToday` (дуэль = «играл сегодня»).
- `games.deleted`: soft-delete. `ActivePendingGame`/`PendingGamesWithCode` фильтруют
  `deleted=0`; `watchGame` останавливается на `g.Deleted` (иначе отменённая дуэль
  могла бы авто-записаться).
- `games.post_msg_id` / `post_text` / `post_kind`: карточка игры в чате, одно
  сообщение, которое редактируется всю игру (`post_text` = база, live-строки
  дописываются к ней; `post_kind` = `''` | `invite`).
- `games.accepted_at` (дуэль): ставится при «Принять» (`AcceptDuel`), при входе цели
  в комнату (`autoAcceptDuel`) и при финализации (`FinalizeGame`, если пусто).
  Миграция бэкфиллит завершённые дуэли. Без него открытый вызов сгорает по таймеру.
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
- `seasons`: **один active на чат**, partial unique index `idx_one_active_season ON
  seasons(chat_id) WHERE status='active'`. Индекс создаётся в `migrate()` ПОСЛЕ
  self-healing дедупа (оставить active-сезон с макс. числом игр), т.к. `schema.sql`
  бежит ДО `migrate()`. `ActiveSeason`/`CloseSeason` используют
  `INSERT ... ON CONFLICT(chat_id) WHERE status='active' DO NOTHING` + re-read
  (read-then-create гонка плодила дубли). `CloseSeason` идемпотентен: архивный сезон
  не трогает (`ErrSeasonNotActive`). Season «номер» (`number`) ≠ row id.
- **Закрытие сезона**, два выхода: `target` очков (100) закрывает сезон сразу
  (`scoreAndCheck`); `deadline` (`closeExpiredSeasonAt`) закрывает, ТОЛЬКО если
  сыграно ≥ `seasons.min_games` (66, `SeasonGameCount`: без дуэлей и удалённых).
  - `deadline` = старт + `SeasonMonths` (2) месяца, локальная полночь, **не**
    по календарным месяцам. 66 игр за 2 месяца = темп ~1.1 игры/день.
  - Не добрали: дедлайн едет на месяц (`ExtendSeasonDeadline`) + пост
    `seasonExtendedText` с недобором; при 0 игр молча (мёртвый чат не пинаем).
  - Добрали, но есть открытая сезонная игра, начатая до дедлайна и младше 3 ч
    (`HasOpenSeasonGame`, `openGameGraceMinutes`): закрытие ждёт следующего тика.
  - `min_games` наследуется `CloseSeason` как `target`, меняется `/settings mingames`.
    Счётчик «N/66» есть в каждом посте результата и на панелях /season и таблицы.
  - Сезон 5 закрылся по календарю на 28 играх (лидер 45/100), откачен вручную:
    отсюда порог по играм.
- `seasons.last_nudge`: маркер `<kind>@<deadline>` разовых постов `nudgeSeason`
  («последняя неделя» со счётчиком игр и недобором, «сегодня финал» только если порог
  игр уже выполнен). Привязан к дедлайну: после продления посты сработают заново.
- `players`: `tg_id` (есть у всех через /join), `username` (часто пусто, не у всех
  есть @ник в Telegram), `usdoku_nick` (для авто-матчинга), `active` (soft-remove).
- `game_rsvp`: ростер «Я в деле» для /invite. `ToggleRsvp` = INSERT OR IGNORE / DELETE.
- **Рейтинг (ELO)**: один на всё, отдельного дуэльного Elo нет. Чистая функция
  истории: `domain.ComputeRatings` прогоняет ВСЕ завершённые игры
  (`sqlCompletedGames`, сезон + дуэли), без таблицы и без инкрементального стейта
  (`storage.GamesForRating` = вход). Вечный (не сбрасывается по сезонам), параллелен
  очкам сезона. Чистый ранк, многопользовательская = попарно (K делится на число
  оппонентов), DNF = поражение финишёрам (DNF-vs-DNF игнор). Старт 1000;
  provisional <10 игр K=64, иначе K=32. Корона и лестница только среди активных
  (`ComputeRatingsActive`), при равенстве корону держит incumbent. Ставки дуэли
  (`DuelStakes`) считаются тем же replay. Дельта пишется в пост результата
  (`ratingFooter`), полная лестница в `/rating`. Edit/delete игры → следующий
  replay сам пересчитает.

## Конвенции
- **Conventional Commits**, БЕЗ `Co-Authored-By` трейлера (стиль репо).
- HTML parse mode: **всё пользовательское через `esc()`**; пинг игрока через `mention()`
  (tg://user?id= ссылка → @username → имя). `@username` пингует надёжнее, но у многих
  его нет → tg-ссылка (уведомляет участников, но рендерится как текст у тех, кого
  клиент не подтянул).
- **Эфемерные сообщения**: служебный шум (ошибки, гарды, валидация, `fail()`) шлётся
  через `b.ephemeral()` (`ephemeral.go`), видит только адресат. Уровни
  (`pickEphemeralRoute`): нажатие кнопки → эфемерное с `callback_query_id` + пустой
  тост (текст один раз); эфемерная команда → эфемерный reply
  (`reply_parameters.ephemeral_message_id`); бот админ чата (кэш 10 мин,
  `botAdminCache`) → эфемерное отправителю, его команда удаляется через 20с;
  иначе или при ошибке API фоллбек: обычное сообщение, само-удаляется через 20с
  вместе с командой. Ценное (результаты/статы/игровые посты с кнопками/
  подтверждения/напоминания) НЕ эфемерно. Callback-тосты (`c.Respond`) уже эфемерны.
- **Эфемерные команды**: `/help`, `/learn`, `/setnick`, `/me` помечены `is_ephemeral`
  в меню (`ephemeralCommands`, все scope). Такая команда приходит с `message_id = 0`
  и `EphemeralMessageID`; хендлер отвечает через `b.privately()` и ничего публичного
  не постит (/help без быстрых кнопок, /me текстом вкладки «Я» без хаба). Эфемерное
  нельзя править `c.Edit`: пикер /learn правится через `b.editCallbackMessage()`
  (`editEphemeralMessageText`). Удалять своё сообщение с `ID = 0` нельзя.
- **Кнопки**: `styled(btn, styleSuccess|styleDanger|stylePrimary)` для главных
  действий (принять/записать/играть, удалить/отказ/отозвать, реванш/ещё одну),
  остальные без стиля. `disabled(btn)`: неактивная кнопка (сгоревший/отозванный
  вызов, открытая вкладка /stats). Старые клиенты всё равно шлют её колбэк, хендлер
  должен это пережить.
- **Тихие посты**: служебное, но не эфемерное (подтверждения, /settings, ростеры)
  шлётся через `b.quiet()` (без уведомления).
- **Личка**: `privateGate` пропускает только start/help/learn/backup (+ /restore
  документом и кнопки справочника), остальное получает отказ. `ensure()` в личке
  возвращает `(nil, nil)`: хендлер должен это проверять.
- Числа со словами: `plural(n, one, few, many)` (`pointsWord`, `gamesWord`,
  `daysWord`). В текстах бота без длинных и средних тире.
- Пикеры/подтверждения через `c.Edit` (трансформируются, не плодят сообщения);
  пикер, который может жить в эфемерном сообщении, через `b.editCallbackMessage()`.
- Тесты: TDD, гоняются в docker. Чистые функции (рендер, scoring, SQL-методы)
  покрыты; Telegram-I/O glue нет.

## Гочи
- Дуэль создаёт игру с `duel_target_id`; отмена дуэли = `SoftDeleteGame` (освобождает
  слот pending-игры; `watchGame` стопается).
- `createGameRoom`: общий core для /newgame, /duel, /invite (guards + usdoku-комната
  + watcher), НЕ постит сообщение (caller рендерит своё). `(nil, nil)` = guard уже
  ответил.
- usdoku-таймстампы: единица не подтверждена; `Player.SolveSeconds()` авто-нормализует
  (>6ч → делит на 1000, считая мс).
- Множественные active-сезона уже были в проде (read-then-create гонка), починено
  индексом + дедупом; `/restore` старого бэкапа лечится self-healing миграцией.
