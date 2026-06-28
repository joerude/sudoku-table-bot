# Redundancy cleanup & conciseness — design

Дата: 2026-06-28

## Контекст и цель

`sudoku-table-bot` — Telegram-бот учёта лиги в судоку для группы ~5 друзей, один
инстанс, один писатель в БД. После серии фич (стрики, бэйджи, недельный дайджест,
дуэли, авто-запись) накопилась избыточность: дублирующиеся пути в UI, повторяющийся
SQL-предикат на каждом сезонном запросе, повторяющийся текст в сообщениях.

Цель сессии — **план действий** по трём направлениям:
1. Сократить редандантность фич/кода/моделей **там, где это реально снижает риск
   ошибок или шум**, не ради метрики «меньше строк».
2. Сделать дизайн storage-слоя безопаснее к изменениям (single source of truth для
   ключевого предиката).
3. Убрать избыточность в сообщениях — они должны быть лаконичнее **без потери
   полезной информации**.

Решение принято осознанно консервативным: проект маленький, читаемость важнее
«идеальной» дедупликации. Часть находок аудита намеренно **отклонена** (см. ниже) —
для бота на 5 человек они были бы overengineering.

## Принятые рамки (из brainstorming + grilling)

| Направление | Решение | Что отклонено и почему |
|---|---|---|
| Команды | Минимум: убрать только алиас `/table` | НЕ убираем quick-menu (онбординг под `/help`), НЕ убираем `/me /speed /duels /history /status` и вкладки `/stats` (держат Telegram-автокомплит, привычки 5 игроков) |
| Сообщения | Дедуп + умеренный трим (4 пункта) | НЕ централизуем «пустые состояния» (контекстны), НE режем инфо-таблицы и `/help` (уже компактный) |
| Storage | Предикаты-константы, полная нормализация алиасов | НЕ трогаем микродубли (SpeedStat/SpeedRow, ChatSettings≈chats, 4 PlayerBy*) — helper'ы там снизят читаемость |

### Что grilling исправил в исходном аудите
- Quick-callbacks (`cbQGame/cbQStatus/cbQMe`) — **не хлам**: это кнопки под `helpText`
  и `welcomeText` (онбординг). Удаление = деградация UX. Оставлены.
- Nick-warning — **не 6+ мест, а 2** реальных «before-game» предупреждения
  (`handlers_game.go:143`, `messages.go:399`). Остальные совпадения — другие концепты
  («не узнал ники» после игры, «время не подтянулось») и сливать их нельзя.
- `/help` — **уже компактный** (19 строк) и сам документирует, что `/status /me…` =
  вкладки `/stats`. Резать нечего.
- Auto-record «мульти-сообщение» — **2 поста, не 3** (`handlers_auto.go:130-135`).
- Централизация empty-state в общий `emptyStateText()` — **отклонена**: тексты
  контекстны, общий вариант сделает их хуже.

## Дизайн по workstream'ам

Три независимых workstream'а. Каждый — отдельный коммит, каждый верифицируется
`go build ./... && go vet ./... && go test ./...` в docker (на хосте нет `go`):

```
docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine \
  sh -c 'go build ./... && go vet ./... && go test ./...'
```

### Workstream 1 — Команды

**Изменение:** удалить строку регистрации алиаса `/table` в `internal/bot/bot.go:132`
(`b.tb.Handle("/table", b.onStatus)`). `/status` остаётся единственным входом к
таблице-команде.

**Риск:** минимальный. Возможна мышечная память у кого-то — приемлемо для 5 человек;
`/status` и вкладка в `/stats` остаются.

**Верификация:** сборка зелёная. `/table` — это только `Handle`-роут, его нет в меню
команд (`SetCommands`), поэтому `TestBotCommandsAreTheSixEveryday` (проверяет
`play/stats/join/setnick/players/help`) не затрагивается.

### Workstream 2 — Сообщения (дедуп + умеренный трим)

**2.1 — Auto-record: 2 поста → 1.**
`internal/bot/handlers_auto.go:127-138`. Сейчас при неузнанных никах шлётся два
`Send`: `autoMappingText` + `recordKeyboard`, затем отдельный «Это твой ник?» +
`claimNickKeyboard`. Объединить в один `Send`: текст `autoMappingText` + единая
inline-клавиатура, содержащая ряды `recordKeyboard` и (если есть unknown) ряды
`claimNickKeyboard`. Нужен хелпер сборки объединённой клавиатуры
(напр. `recordAndClaimKeyboard(gameID, unknown)`).

**2.2 — Общий `missingNickWarn(names []string) string`.**
Новый билдер в `messages.go`. Возвращает единый текст предупреждения «без ника»
(со списком имён). Применить в:
- `handlers_game.go:141-145` (newgame) — вместо инлайн-`fmt.Sprintf`;
- `messages.go:398-400` (`duelChallengeText`, ветка `nickWarn`) — передавать имена
  и вызывать общий билдер. (`duelChallengeText` сменит сигнатуру: `nickWarn bool` →
  `missingNicks []string`; пустой срез = нет предупреждения.)

Текст должен сохранять подстроку `setnick` (этого требует `TestDuelChallengeText` /
`messages_test.go:353`).

**2.3 — Трим инструкций.**
- `newGameText` (`messages.go:79-87`): убрать пошаговый «1./2./3.», оставить заголовок,
  ссылку на создание комнаты и строку «Когда доиграете — жми „📝 Записать
  результат“». `newGameWithCodeText` остаётся отдельной функцией (другое состояние:
  комната уже создана).
- `welcomeText` (`messages.go:38-45`): сжать «С чего начать» до 1–2 строк, сохранив
  `/join`, `/setnick`, упоминание `/help`.

**2.4 — Константы callback-гардов.**
В `messages.go` (или `bot.go`) ввести именованные константы для повторяющихся
ответов: «Сначала /join», «Игра не найдена или закрыта», generic-ошибка. Заменить
инлайн-литералы в `handlers_game.go`, `handlers_duel.go`, `handlers_invite.go`,
`handlers_auto.go`. Текст унифицировать (напр. «Игра не найдена» и «Игра уже закрыта»
→ одна константа «Игра не найдена или закрыта»).

**Не трогаем:** `standingsText`, `resultText`, `duelsText`, `speedText`, `meText`,
`digestText`, `historyText`, `recordsText`, `helpText`, пустые состояния
(`/export`, `/weekly`, speed «мало данных»).

**Верификация:** обновить затронутые тексты-тесты в `messages_test.go`; добавить
тесты на `missingNickWarn` и на объединённую клавиатуру (TDD). Остальные
message-тесты остаются зелёными.

### Workstream 3 — Storage (предикаты-константы)

**Проблема:** предикат «завершённая, не удалённая, сезонная игра» написан вручную в
~16 местах (`stats.go`, `standings.go`, `duels.go`, `reminders.go`), с непоследовательным
алиасом (`g.` vs bare). Забыть `duel_target_id IS NULL` = тихий баг в очках.

**Дизайн:** в `internal/storage/predicates.go` (новый файл) ввести строковые
константы:

```go
// игры, попадающие в любую статистику: завершённые и не удалённые.
const sqlCompletedGames = "g.status = 'completed' AND g.deleted = 0"
// сезонные (не-дуэльные) игры.
const sqlSeasonalGames = sqlCompletedGames + " AND g.duel_target_id IS NULL"
// дуэльные игры.
const sqlDuelGames = sqlCompletedGames + " AND g.duel_target_id IS NOT NULL"
```

(Это compile-time константы, не пользовательский ввод — конкатенация в SQL безопасна.)

**Полная нормализация:** привести все ~16 запросов к алиасу `g` для таблицы `games`
(где сейчас bare — добавить `games g` / `g.` префиксы), затем подставить константы:
- сезонные запросы (`Standings`, `SpeedFor`, `Speedboard`, `ExportGames`,
  `RecentGames`, `RecordsBoard`, `RecentRanks`, `PlayedTimes`, `CareerStats`,
  `StatFor`, `GamesSince`, `FastestSince`) → `sqlSeasonalGames`;
- дуэльные (`DuelRecord`, `DuelLeaderboard`, `RecentDuels`, `HeadToHead`) →
  `sqlDuelGames`;
- **`CompletedToday`** (`reminders.go:128`) → `sqlCompletedGames` **только** (без
  duel-фильтра — дуэль считается «играл сегодня», см. CLAUDE.md). Это и есть фикс
  класса ошибок: предикат теперь явно выбран, а не собран вручную.

**Не входит:** N+1 в `RecentGames`/`finishOrder`, хидрация Game-структуры,
объединение SpeedStat/SpeedRow — отложено (пользователь выбрал «только константы»).

**Риск:** нормализация алиасов — поведение-сохраняющая правка; ловит ошибки набор
тестов. Особое внимание: `CompletedToday` НЕ должен получить duel-фильтр.

**Верификация:** `go test ./internal/storage/...` зелёный до и после. Ключевые
стражи: `TestSeasonExcludesDuels`, `TestSpeedboardIgnoresDeletedGames`,
`TestDuelRecord`, `TestDuelLeaderboard`, `TestHeadToHead`,
`TestGameFlowAndStandings`, `TestWeeklyDigestRoundTrip`.

## Порядок исполнения

1. **Workstream 3 (storage)** — поведение-сохраняющий, полностью под тестами; делает
   базу безопаснее для последующих правок. Коммит + docker-тест.
2. **Workstream 2 (сообщения)** — TDD: тесты на новые хелперы, затем правка. Коммит +
   docker-тест.
3. **Workstream 1 (`/table`)** — тривиальная правка. Коммит + docker-тест.

Conventional Commits, без `Co-Authored-By` (стиль репо).

## Критерии готовности

- `go build ./... && go vet ./... && go test ./...` зелёный в docker после каждого
  коммита.
- `CompletedToday` по-прежнему считает дуэли (тест/ручная проверка предиката).
- Сезонные запросы по-прежнему исключают дуэли (`TestSeasonExcludesDuels`).
- Auto-record при неузнанных никах шлёт один пост с обеими группами кнопок.
- `missingNickWarn` используется и в newgame, и в duel; вывод содержит `setnick`.
- `/table` больше не зарегистрирован; `/status` работает.
- Ни одна инфо-таблица и `/help` не изменены по содержанию.

## Нерешённое / отложено (явно вне скоупа)

- Микродубли storage (SpeedStat/SpeedRow, ChatSettings≈chats, 4 `PlayerBy*`).
- N+1 в `/history`, хидрация полей Game-структуры.
- Любые изменения поверхности команд сверх `/table`.
- Агрессивный трим инфо-сообщений.
