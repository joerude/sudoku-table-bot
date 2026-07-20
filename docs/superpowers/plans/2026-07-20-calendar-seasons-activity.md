# Calendar Seasons + Activity Tab Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Сезон заканчивается по календарю (~месяц), а не только по достижению 100 очков; в `/stats` появляется вкладка «Актив» — кто зовёт играть.

**Architecture:** Новая колонка `seasons.deadline` (UTC). Чистые функции расчёта дедлайна живут в `internal/domain`, минутный тикер `runReminders` закрывает просроченные сезоны (или продлевает пустые). Дедлайн у существующих сезонов заполняется лениво тем же тикером — миграция TZ не знает. Вкладка «Актив» — только SQL по существующей `games.created_by`, новых таблиц нет.

**Tech Stack:** Go 1.25, `gopkg.in/telebot.v3`, `modernc.org/sqlite` (без CGO), сырой SQL через `database/sql`.

## Global Constraints

- **Go на хосте нет.** Любой go-тулинг — только через docker:
  `docker run --rm -v "$PWD":/src -w /src -v gomodcache:/go/pkg/mod -v gobuildcache:/root/.cache/go-build golang:1.25-alpine sh -c '<команда>'`
- **Conventional Commits, БЕЗ трейлера `Co-Authored-By`** (стиль репо).
- HTML parse mode: всё пользовательское — через `esc()`.
- Тесты в контейнере **не могут** полагаться на tzdata (`golang:1.25-alpine` без `tzdata`): в тестах использовать `time.FixedZone("UTC+6", 6*3600)`, НЕ `time.LoadLocation`.
- Время в БД — UTC в формате `2006-01-02 15:04:05` (как `datetime('now')`).
- Предикаты «какие игры считаются» брать из `internal/storage/predicates.go`
  (`sqlCompletedGames`, `sqlSeasonalGames`, `sqlDuelGames`), не писать условия руками.
- Прод-чат: `-1002977114229`, TZ `Asia/Bishkek`. Два других чата в БД мёртвые (0 сезонных игр) — правило «пустой сезон продлевается молча» гарантирует, что бот туда не пишет.

---

### Task 1: Расчёт дедлайна сезона (чистые функции)

**Files:**
- Create: `internal/domain/season_deadline.go`
- Test: `internal/domain/season_deadline_test.go`

**Interfaces:**
- Consumes: ничего (stdlib `time`).
- Produces:
  - `domain.SeasonDeadline(now time.Time, loc *time.Location) time.Time` — момент окончания сезона, созданного в `now`; возвращает **UTC**.
  - `domain.ExtendSeasonDeadline(deadline, now time.Time, loc *time.Location) time.Time` — сдвигает просроченный дедлайн вперёд целыми месяцами, пока он не окажется в будущем; возвращает **UTC**.

- [ ] **Step 1: Write the failing test**

Создать `internal/domain/season_deadline_test.go`:

```go
package domain

import (
	"testing"
	"time"
)

// bishkek is a fixed +06:00 zone: the test container has no tzdata, so
// LoadLocation("Asia/Bishkek") would silently fall back to UTC.
var bishkek = time.FixedZone("UTC+6", 6*3600)

func TestSeasonDeadlineIsNextMonthStart(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, bishkek)
	want := time.Date(2026, 8, 1, 0, 0, 0, 0, bishkek).UTC()
	if got := SeasonDeadline(now, bishkek); !got.Equal(want) {
		t.Errorf("SeasonDeadline = %s, want %s", got, want)
	}
}

func TestSeasonDeadlineSkipsStubSeason(t *testing.T) {
	// 28 July: less than 7 days left, so the deadline jumps to 1 September.
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, bishkek)
	want := time.Date(2026, 9, 1, 0, 0, 0, 0, bishkek).UTC()
	if got := SeasonDeadline(now, bishkek); !got.Equal(want) {
		t.Errorf("SeasonDeadline = %s, want %s", got, want)
	}
}

func TestSeasonDeadlineCrossesYear(t *testing.T) {
	now := time.Date(2026, 12, 5, 9, 0, 0, 0, bishkek)
	want := time.Date(2027, 1, 1, 0, 0, 0, 0, bishkek).UTC()
	if got := SeasonDeadline(now, bishkek); !got.Equal(want) {
		t.Errorf("SeasonDeadline = %s, want %s", got, want)
	}
}

func TestExtendSeasonDeadlineOneMonth(t *testing.T) {
	deadline := time.Date(2026, 8, 1, 0, 0, 0, 0, bishkek).UTC()
	now := time.Date(2026, 8, 1, 0, 1, 0, 0, bishkek)
	want := time.Date(2026, 9, 1, 0, 0, 0, 0, bishkek).UTC()
	if got := ExtendSeasonDeadline(deadline, now, bishkek); !got.Equal(want) {
		t.Errorf("ExtendSeasonDeadline = %s, want %s", got, want)
	}
}

func TestExtendSeasonDeadlineSkipsLongOutage(t *testing.T) {
	// Bot was down from August to November: the deadline must land ahead of now.
	deadline := time.Date(2026, 8, 1, 0, 0, 0, 0, bishkek).UTC()
	now := time.Date(2026, 11, 10, 0, 0, 0, 0, bishkek)
	want := time.Date(2026, 12, 1, 0, 0, 0, 0, bishkek).UTC()
	if got := ExtendSeasonDeadline(deadline, now, bishkek); !got.Equal(want) {
		t.Errorf("ExtendSeasonDeadline = %s, want %s", got, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
docker run --rm -v "$PWD":/src -w /src -v gomodcache:/go/pkg/mod -v gobuildcache:/root/.cache/go-build \
  golang:1.25-alpine sh -c 'go test ./internal/domain/ -run TestSeasonDeadline -v'
```

Expected: FAIL — `undefined: SeasonDeadline`.

- [ ] **Step 3: Write minimal implementation**

Создать `internal/domain/season_deadline.go`:

```go
package domain

import "time"

// minSeasonDays is the shortest a fresh season may be. A season opened in the
// tail of a month would otherwise be decided in a couple of days, so its
// deadline skips to the end of the following month instead.
const minSeasonDays = 7

// nextMonthStart returns midnight starting the calendar month after t, in loc.
func nextMonthStart(t time.Time, loc *time.Location) time.Time {
	t = t.In(loc)
	return time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, loc)
}

// SeasonDeadline returns (in UTC) when a season created at now must end: the
// midnight that starts the next calendar month in loc, pushed one month further
// when that is less than minSeasonDays away.
func SeasonDeadline(now time.Time, loc *time.Location) time.Time {
	d := nextMonthStart(now, loc)
	if d.Sub(now) < minSeasonDays*24*time.Hour {
		d = nextMonthStart(d, loc)
	}
	return d.UTC()
}

// ExtendSeasonDeadline moves an expired deadline forward by whole months until
// it is in the future. Used when a season's month ended with nothing played,
// and to recover after an outage that swallowed several deadlines.
func ExtendSeasonDeadline(deadline, now time.Time, loc *time.Location) time.Time {
	d := deadline
	for !d.After(now) {
		d = nextMonthStart(d, loc)
	}
	return d.UTC()
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
docker run --rm -v "$PWD":/src -w /src -v gomodcache:/go/pkg/mod -v gobuildcache:/root/.cache/go-build \
  golang:1.25-alpine sh -c 'go test ./internal/domain/ -v -run "SeasonDeadline"'
```

Expected: PASS — все 5 тестов `ok`.

- [ ] **Step 5: Commit**

```bash
git add internal/domain/season_deadline.go internal/domain/season_deadline_test.go
git commit -m "feat(season): pure calendar-month deadline math"
```

---

### Task 2: Хранение дедлайна (колонка, миграция, чтение/запись)

**Files:**
- Modify: `internal/storage/schema.sql` (таблица `seasons`, ~строка 25-35)
- Modify: `internal/storage/store.go` (список в `migrate()`)
- Modify: `internal/storage/seasons.go` (struct `Season`, `activeSeasonRow`, `SeasonByNumber`, `SeasonByID`, `scanSeason`, новый `SetSeasonDeadline`)
- Test: `internal/storage/season_deadline_test.go`

**Interfaces:**
- Consumes: ничего из Task 1 (слой БД про время не знает — хранит строку).
- Produces:
  - поле `storage.Season.Deadline sql.NullString` — UTC `2006-01-02 15:04:05`, невалидно = дедлайн ещё не проставлен;
  - `(*Store).SetSeasonDeadline(seasonID int64, deadline string) error`.

**Важно:** сигнатуру `CloseSeason(se *Season, winnerID int64)` НЕ менять — 6 тестов и один прод-вызов её используют. Дедлайн новому сезону ставит вызывающий (Task 3) через `SetSeasonDeadline`; если запись сорвётся, ленивое заполнение в тикере починит на следующей минуте.

- [ ] **Step 1: Write the failing test**

Создать `internal/storage/season_deadline_test.go`:

```go
package storage

import (
	"path/filepath"
	"testing"
)

// TestSeasonDeadlineRoundTrip: a fresh season has no deadline; once set it is
// read back by every season getter and survives reopening the store.
func TestSeasonDeadlineRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.db")
	st, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	const chat = int64(-4001)
	if err := st.EnsureChat(chat, 1); err != nil {
		t.Fatalf("EnsureChat: %v", err)
	}
	se, err := st.ActiveSeason(chat)
	if err != nil {
		t.Fatalf("ActiveSeason: %v", err)
	}
	if se.Deadline.Valid {
		t.Errorf("fresh season: Deadline = %q, want unset", se.Deadline.String)
	}

	const dl = "2026-07-31 18:00:00"
	if err := st.SetSeasonDeadline(se.ID, dl); err != nil {
		t.Fatalf("SetSeasonDeadline: %v", err)
	}

	got, err := st.ActiveSeason(chat)
	if err != nil {
		t.Fatalf("ActiveSeason after set: %v", err)
	}
	if !got.Deadline.Valid || got.Deadline.String != dl {
		t.Errorf("ActiveSeason: Deadline = %v/%q, want %q", got.Deadline.Valid, got.Deadline.String, dl)
	}
	byID, err := st.SeasonByID(se.ID)
	if err != nil {
		t.Fatalf("SeasonByID: %v", err)
	}
	if byID.Deadline.String != dl {
		t.Errorf("SeasonByID: Deadline = %q, want %q", byID.Deadline.String, dl)
	}
	byNum, err := st.SeasonByNumber(chat, se.Number)
	if err != nil {
		t.Fatalf("SeasonByNumber: %v", err)
	}
	if byNum.Deadline.String != dl {
		t.Errorf("SeasonByNumber: Deadline = %q, want %q", byNum.Deadline.String, dl)
	}

	// Reopening runs schema.sql + migrate() again: the column must survive.
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	st2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { st2.Close() })
	again, err := st2.ActiveSeason(chat)
	if err != nil {
		t.Fatalf("ActiveSeason after reopen: %v", err)
	}
	if again.Deadline.String != dl {
		t.Errorf("after reopen: Deadline = %q, want %q", again.Deadline.String, dl)
	}
}

// TestCloseSeasonLeavesDeadlineUnset: the next season starts without a
// deadline, so the bot's lazy fill assigns a fresh one.
func TestCloseSeasonLeavesDeadlineUnset(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-4002)
	if err := st.EnsureChat(chat, 1); err != nil {
		t.Fatalf("EnsureChat: %v", err)
	}
	se, err := st.ActiveSeason(chat)
	if err != nil {
		t.Fatalf("ActiveSeason: %v", err)
	}
	if err := st.SetSeasonDeadline(se.ID, "2026-07-31 18:00:00"); err != nil {
		t.Fatalf("SetSeasonDeadline: %v", err)
	}
	next, err := st.CloseSeason(se, 0)
	if err != nil {
		t.Fatalf("CloseSeason: %v", err)
	}
	if next.Deadline.Valid {
		t.Errorf("next season: Deadline = %q, want unset", next.Deadline.String)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
docker run --rm -v "$PWD":/src -w /src -v gomodcache:/go/pkg/mod -v gobuildcache:/root/.cache/go-build \
  golang:1.25-alpine sh -c 'go test ./internal/storage/ -run "SeasonDeadline|CloseSeasonLeaves" -v'
```

Expected: FAIL (build) — `se.Deadline undefined`, `st.SetSeasonDeadline undefined`.

- [ ] **Step 3: Add the column to the schema**

В `internal/storage/schema.sql`, в `CREATE TABLE IF NOT EXISTS seasons`, после строки `started_at   TEXT    NOT NULL DEFAULT (datetime('now')),` добавить:

```sql
    deadline     TEXT,                            -- UTC; season also ends when this passes
```

- [ ] **Step 4: Add the migration for existing databases**

В `internal/storage/store.go`, в список `migrate()`, после строки
`` `ALTER TABLE games ADD COLUMN declined_at TEXT`, // duel: target pressed «Отказ» ``
добавить:

```go
		// Calendar season end. Left NULL on purpose: the deadline depends on the
		// chat timezone, which this layer doesn't know — the reminder tick fills
		// it in on the next minute.
		`ALTER TABLE seasons ADD COLUMN deadline TEXT`,
```

- [ ] **Step 5: Read and write the column**

В `internal/storage/seasons.go`:

(a) в struct `Season` после `Status string` добавить поле:

```go
	// Deadline is when the season ends by the calendar (UTC "2006-01-02 15:04:05").
	// Invalid = not assigned yet; the bot's reminder tick fills it in.
	Deadline sql.NullString
```

(b) в трёх запросах добавить `deadline` в список колонок — в `activeSeasonRow`, `SeasonByNumber`, `SeasonByID` заменить
`SELECT id, chat_id, number, target, points_table, status FROM seasons`
на
`SELECT id, chat_id, number, target, points_table, status, deadline FROM seasons`
(остальная часть каждого запроса — без изменений).

(c) в `scanSeason` добавить `&se.Deadline` в конец `row.Scan(...)`:

```go
	if err := row.Scan(&se.ID, &se.ChatID, &se.Number, &se.Target, &ptsJSON, &se.Status, &se.Deadline); err != nil {
		return nil, err
	}
```

(d) после `UpdateSeasonTarget` добавить сеттер:

```go
// SetSeasonDeadline stores when the season ends by the calendar (UTC
// "2006-01-02 15:04:05").
func (s *Store) SetSeasonDeadline(seasonID int64, deadline string) error {
	_, err := s.db.Exec(`UPDATE seasons SET deadline=? WHERE id=?`, deadline, seasonID)
	return err
}
```

- [ ] **Step 6: Run the whole storage suite**

```bash
docker run --rm -v "$PWD":/src -w /src -v gomodcache:/go/pkg/mod -v gobuildcache:/root/.cache/go-build \
  golang:1.25-alpine sh -c 'go test ./internal/storage/ -v -run "SeasonDeadline|CloseSeasonLeaves" && go test ./internal/storage/'
```

Expected: новые тесты PASS, весь пакет `ok` (регрессий в существующих сезонных тестах нет).

- [ ] **Step 7: Commit**

```bash
git add internal/storage/schema.sql internal/storage/store.go internal/storage/seasons.go internal/storage/season_deadline_test.go
git commit -m "feat(season): store season deadline (nullable, filled by the bot)"
```

---

### Task 3: Закрытие сезона по дедлайну

**Files:**
- Modify: `internal/bot/reminders.go` (новая функция + вызов в `runReminders`)
- Modify: `internal/bot/messages.go` (рядом с `parseDBTime`, ~строка 919: `fmtDBTime`; рядом с `seasonEndText`, ~строка 718: `seasonDeadlineEndText`)
- Modify: `internal/bot/handlers_game.go` (`scoreAndCheck`, ~строка 607 — дедлайн новому сезону после досрочного финала)
- Test: `internal/bot/messages_test.go` (дописать тесты в конец файла)

**Interfaces:**
- Consumes: `domain.SeasonDeadline`, `domain.ExtendSeasonDeadline` (Task 1); `storage.Season.Deadline`, `(*Store).SetSeasonDeadline` (Task 2); существующие `(*Store).AllChats`, `(*Store).ActiveSeason`, `(*Store).Standings`, `(*Store).SeasonMeta`, `(*Store).CloseSeason`, `(*Bot).seasonAwards`, `loadLoc`, `parseDBTime`.
- Produces:
  - `fmtDBTime(t time.Time) string` — UTC в формате БД;
  - `seasonDeadlineEndText(number int, winner string, points int, awards []string, nextNumber, nextTarget int, nextDeadline time.Time, loc *time.Location) string`;
  - `(*Bot).closeExpiredSeasons()`.

- [ ] **Step 1: Write the failing test**

Дописать в конец `internal/bot/messages_test.go`:

```go
func TestFmtDBTime(t *testing.T) {
	loc := time.FixedZone("UTC+6", 6*3600)
	got := fmtDBTime(time.Date(2026, 8, 1, 0, 0, 0, 0, loc))
	if want := "2026-07-31 18:00:00"; got != want {
		t.Errorf("fmtDBTime = %q, want %q", got, want)
	}
}

func TestSeasonDeadlineEndText(t *testing.T) {
	loc := time.FixedZone("UTC+6", 6*3600)
	next := time.Date(2026, 9, 1, 0, 0, 0, 0, loc).UTC()
	got := seasonDeadlineEndText(5, "Nur", 42, []string{"🏅 award"}, 6, 100, next, loc)

	for _, want := range []string{
		"Сезон 5 завершён",
		"по календарю",
		"<b>Nur</b>",
		"42",
		"🏅 award",
		"сезон 6",
		"100",
		"31 августа",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("seasonDeadlineEndText missing %q in:\n%s", want, got)
		}
	}
}

func TestSeasonDeadlineEndTextNoAwards(t *testing.T) {
	loc := time.FixedZone("UTC+6", 6*3600)
	next := time.Date(2026, 9, 1, 0, 0, 0, 0, loc).UTC()
	got := seasonDeadlineEndText(1, "Joe <b>", 3, nil, 2, 100, next, loc)
	if strings.Contains(got, "Номинации") {
		t.Errorf("no awards, but the awards block rendered:\n%s", got)
	}
	if !strings.Contains(got, "Joe &lt;b&gt;") {
		t.Errorf("winner name not escaped:\n%s", got)
	}
}
```

Примечание: дедлайн следующего сезона — 1 сентября 00:00 местного, то есть **последний игровой день — 31 августа**; текст показывает именно его.

Проверить, что в `messages_test.go` есть импорты `strings` и `time`; если `time` нет — добавить в блок импортов.

- [ ] **Step 2: Run test to verify it fails**

```bash
docker run --rm -v "$PWD":/src -w /src -v gomodcache:/go/pkg/mod -v gobuildcache:/root/.cache/go-build \
  golang:1.25-alpine sh -c 'go test ./internal/bot/ -run "FmtDBTime|SeasonDeadlineEndText" -v'
```

Expected: FAIL (build) — `undefined: fmtDBTime`, `undefined: seasonDeadlineEndText`.

- [ ] **Step 3: Implement the renderers**

В `internal/bot/messages.go` сразу после `parseDBTime` добавить:

```go
// fmtDBTime renders a time as a SQLite datetime('now') string (UTC).
func fmtDBTime(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05")
}

// ruMonthsGen are Russian month names in the genitive case ("1 августа").
var ruMonthsGen = [...]string{
	"января", "февраля", "марта", "апреля", "мая", "июня",
	"июля", "августа", "сентября", "октября", "ноября", "декабря",
}

// fmtRuDate renders a UTC instant as "31 августа" in loc.
func fmtRuDate(t time.Time, loc *time.Location) string {
	t = t.In(loc)
	return fmt.Sprintf("%d %s", t.Day(), ruMonthsGen[int(t.Month())-1])
}
```

В `internal/bot/messages.go` сразу после `seasonEndText` добавить:

```go
// seasonDeadlineEndText announces a season closed by the calendar rather than
// by reaching the target. nextDeadline is the new season's end instant (UTC);
// the text shows the last playable day, i.e. the day before it.
func seasonDeadlineEndText(number int, winner string, points int, awards []string,
	nextNumber, nextTarget int, nextDeadline time.Time, loc *time.Location) string {
	var b strings.Builder
	fmt.Fprintf(&b, "📅🏆 <b>Сезон %d завершён по календарю!</b>\nПобедитель: <b>%s</b> (%d очков) 👑\n",
		number, esc(winner), points)
	if len(awards) > 0 {
		b.WriteString("\n<b>Номинации сезона</b>\n" + strings.Join(awards, "\n") + "\n")
	}
	fmt.Fprintf(&b, "\nНачат <b>сезон %d</b> — все с нуля. Цель: %d очков, финал %s. Поехали! 🔥",
		nextNumber, nextTarget, fmtRuDate(nextDeadline.Add(-time.Second), loc))
	return b.String()
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
docker run --rm -v "$PWD":/src -w /src -v gomodcache:/go/pkg/mod -v gobuildcache:/root/.cache/go-build \
  golang:1.25-alpine sh -c 'go test ./internal/bot/ -run "FmtDBTime|SeasonDeadlineEndText" -v'
```

Expected: PASS (3 теста).

- [ ] **Step 5: Implement the tick**

В `internal/bot/reminders.go` в `runReminders` добавить четвёртый шаг:

```go
	for range ticker.C {
		b.remindStalePending()
		b.remindDaily()
		b.remindWeekly()
		b.closeExpiredSeasons()
	}
```

В конец `internal/bot/reminders.go` добавить:

```go
// closeExpiredSeasons ends seasons whose calendar deadline has passed, and
// assigns a deadline to seasons that have none yet (older rows, and the season
// opened by CloseSeason). A season whose month produced no points is extended
// silently instead of crowning a champion with nothing — that also keeps the
// bot quiet in chats that no longer play.
func (b *Bot) closeExpiredSeasons() {
	chats, err := b.st.AllChats()
	if err != nil {
		log.Printf("closeExpiredSeasons: %v", err)
		return
	}
	for _, ch := range chats {
		if err := b.closeExpiredSeason(ch); err != nil {
			log.Printf("closeExpiredSeasons %d: %v", ch.ChatID, err)
		}
	}
}

// closeExpiredSeason handles one chat; separated so a failure in one chat
// doesn't skip the others.
func (b *Bot) closeExpiredSeason(ch storage.ChatSettings) error {
	season, err := b.st.ActiveSeason(ch.ChatID)
	if err != nil {
		return err
	}
	loc := loadLoc(ch.TZ)
	now := time.Now()

	if !season.Deadline.Valid || season.Deadline.String == "" {
		return b.st.SetSeasonDeadline(season.ID, fmtDBTime(domain.SeasonDeadline(now, loc)))
	}
	deadline, err := parseDBTime(season.Deadline.String)
	if err != nil { // unreadable value: rewrite it rather than retry every minute
		return b.st.SetSeasonDeadline(season.ID, fmtDBTime(domain.SeasonDeadline(now, loc)))
	}
	if now.UTC().Before(deadline) {
		return nil
	}

	standings, err := b.st.Standings(ch.ChatID, season.ID)
	if err != nil {
		return err
	}
	if len(standings) == 0 || standings[0].Points == 0 {
		// Nothing was played (or nothing scored): roll the deadline forward.
		ext := domain.ExtendSeasonDeadline(deadline, now.UTC(), loc)
		log.Printf("📅 chat %d: empty season %d extended to %s", ch.ChatID, season.Number, ext)
		return b.st.SetSeasonDeadline(season.ID, fmtDBTime(ext))
	}

	awards := b.seasonAwards(ch.ChatID, season.ID, standings)
	newSeason, err := b.st.CloseSeason(season, standings[0].PlayerID)
	if err != nil {
		return err
	}
	nextDeadline := domain.SeasonDeadline(now, loc)
	if err := b.st.SetSeasonDeadline(newSeason.ID, fmtDBTime(nextDeadline)); err != nil {
		log.Printf("closeExpiredSeason.setdeadline: %v", err) // next tick fills it in
	}
	log.Printf("🏁 season %d closed by deadline, winner=%s (%d pts) → season %d",
		season.Number, standings[0].Name, standings[0].Points, newSeason.Number)
	_, err = b.tb.Send(tele.ChatID(ch.ChatID),
		seasonDeadlineEndText(season.Number, standings[0].Name, standings[0].Points,
			awards, newSeason.Number, newSeason.Target, nextDeadline, loc))
	return err
}
```

Добавить в импорты `internal/bot/reminders.go` пакет `"github.com/joerude/sudoku-bot-telegram/internal/storage"` (для `storage.ChatSettings`); `domain`, `log`, `time`, `tele` там уже есть.

- [ ] **Step 6: Give the early-finish path a deadline too**

В `internal/bot/handlers_game.go`, в `scoreAndCheck`, после успешного `CloseSeason` (строка вида `newSeason, err := b.st.CloseSeason(season, standings[0].PlayerID)` и её проверки ошибки) — перед вычислением `awards` — добавить:

```go
		if derr := b.st.SetSeasonDeadline(newSeason.ID,
			fmtDBTime(domain.SeasonDeadline(time.Now(), loadLoc(b.chatTZ(game.ChatID))))); derr != nil {
			log.Printf("scoreAndCheck.setdeadline: %v", derr) // the reminder tick fills it in
		}
```

(`domain`, `log`, `time` в этом файле уже импортированы.)

- [ ] **Step 7: Build, vet, full test run**

```bash
docker run --rm -v "$PWD":/src -w /src -v gomodcache:/go/pkg/mod -v gobuildcache:/root/.cache/go-build \
  golang:1.25-alpine sh -c 'go build ./... && go vet ./... && go test ./...'
```

Expected: сборка чистая, `go vet` без замечаний, все пакеты `ok`.

- [ ] **Step 8: Commit**

```bash
git add internal/bot/reminders.go internal/bot/messages.go internal/bot/messages_test.go internal/bot/handlers_game.go
git commit -m "feat(season): close a season when its calendar month ends

The season only ever ended at 100 points, so a slow month stretched it
indefinitely. A season now also ends when its deadline passes: the leader
takes it, a month with no points rolls the deadline forward instead."
```

---

### Task 4: Дедлайн виден в таблице и в /season

**Files:**
- Modify: `internal/bot/messages.go` (новая `seasonDeadlineLine` рядом с `standingsText`, ~строка 397)
- Modify: `internal/bot/handlers_stats.go` (ветка `default` в `statsView`, ~строка 443; `onSeason`, ~строка 110)
- Test: `internal/bot/messages_test.go`

**Interfaces:**
- Consumes: `storage.Season.Deadline` (Task 2), `parseDBTime`, `fmtRuDate` (Task 3).
- Produces: `seasonDeadlineLine(se *storage.Season, now time.Time, loc *time.Location) string` — строка вида `\n\n📅 Финал сезона: 31 августа (через 12 дн)`; пустая строка, когда дедлайна нет.

- [ ] **Step 1: Write the failing test**

Дописать в конец `internal/bot/messages_test.go`:

```go
func TestSeasonDeadlineLine(t *testing.T) {
	loc := time.FixedZone("UTC+6", 6*3600)
	se := &storage.Season{
		Number:   5,
		Deadline: sql.NullString{String: "2026-07-31 18:00:00", Valid: true}, // 1 Aug 00:00 +06
	}
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, loc)
	got := seasonDeadlineLine(se, now, loc)
	if !strings.Contains(got, "31 июля") {
		t.Errorf("line = %q, want the last playable day (31 июля)", got)
	}
	if !strings.Contains(got, "11 дн") {
		t.Errorf("line = %q, want 11 days left", got)
	}
}

func TestSeasonDeadlineLineLastDay(t *testing.T) {
	loc := time.FixedZone("UTC+6", 6*3600)
	se := &storage.Season{
		Number:   5,
		Deadline: sql.NullString{String: "2026-07-31 18:00:00", Valid: true},
	}
	now := time.Date(2026, 7, 31, 20, 0, 0, 0, loc)
	got := seasonDeadlineLine(se, now, loc)
	if !strings.Contains(got, "сегодня") {
		t.Errorf("line = %q, want it to say the season ends today", got)
	}
}

func TestSeasonDeadlineLineUnset(t *testing.T) {
	se := &storage.Season{Number: 5}
	if got := seasonDeadlineLine(se, time.Now(), time.UTC); got != "" {
		t.Errorf("no deadline: line = %q, want empty", got)
	}
}
```

Проверить импорты `messages_test.go`: нужны `database/sql`, `strings`, `time` и пакет `storage` — добавить недостающие.

- [ ] **Step 2: Run test to verify it fails**

```bash
docker run --rm -v "$PWD":/src -w /src -v gomodcache:/go/pkg/mod -v gobuildcache:/root/.cache/go-build \
  golang:1.25-alpine sh -c 'go test ./internal/bot/ -run SeasonDeadlineLine -v'
```

Expected: FAIL (build) — `undefined: seasonDeadlineLine`.

- [ ] **Step 3: Implement the line**

В `internal/bot/messages.go` сразу после `standingsText` добавить:

```go
// seasonDeadlineLine renders when the season ends by the calendar, as a block
// to append to a season panel. Empty when the season has no deadline yet.
func seasonDeadlineLine(se *storage.Season, now time.Time, loc *time.Location) string {
	if se == nil || !se.Deadline.Valid || se.Deadline.String == "" {
		return ""
	}
	deadline, err := parseDBTime(se.Deadline.String)
	if err != nil {
		return ""
	}
	// The deadline is midnight opening the next month, so the last playable
	// day is the one before it.
	last := deadline.Add(-time.Second)
	days := int(last.Sub(now.UTC()).Hours() / 24)
	switch {
	case days < 0:
		return fmt.Sprintf("\n\n📅 Финал сезона: %s", fmtRuDate(last, loc))
	case days == 0:
		return fmt.Sprintf("\n\n📅 Финал сезона: %s — <b>сегодня!</b>", fmtRuDate(last, loc))
	default:
		return fmt.Sprintf("\n\n📅 Финал сезона: %s (через %d дн)", fmtRuDate(last, loc), days)
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
docker run --rm -v "$PWD":/src -w /src -v gomodcache:/go/pkg/mod -v gobuildcache:/root/.cache/go-build \
  golang:1.25-alpine sh -c 'go test ./internal/bot/ -run SeasonDeadlineLine -v'
```

Expected: PASS (3 теста).

- [ ] **Step 5: Wire it into the two panels**

В `internal/bot/handlers_stats.go`, в `statsView`, ветка `default` — заменить

```go
		text = standingsText(season, standings)
```

на

```go
		text = standingsText(season, standings) +
			seasonDeadlineLine(season, time.Now(), loadLoc(b.chatTZ(chatID)))
```

В `internal/bot/handlers_stats.go`, в `onSeason` (последняя строка функции, ~строка 133) — заменить

```go
	return c.Send(seasonText(season, leader)+archiveHint(nums), seasonsKeyboard(nums, season.Number))
```

на

```go
	text := seasonText(season, leader) +
		seasonDeadlineLine(season, time.Now(), loadLoc(b.chatTZ(c.Chat().ID))) +
		archiveHint(nums)
	return c.Send(text, seasonsKeyboard(nums, season.Number))
```

Архивные сезоны (`seasonSummaryView`, `onSeasonView`) НЕ трогать — там дедлайн уже неактуален.

Проверить, что `time` импортирован в `handlers_stats.go`; если нет — добавить.

- [ ] **Step 6: Build, vet, full test run**

```bash
docker run --rm -v "$PWD":/src -w /src -v gomodcache:/go/pkg/mod -v gobuildcache:/root/.cache/go-build \
  golang:1.25-alpine sh -c 'go build ./... && go vet ./... && go test ./...'
```

Expected: всё зелёное.

- [ ] **Step 7: Commit**

```bash
git add internal/bot/messages.go internal/bot/messages_test.go internal/bot/handlers_stats.go
git commit -m "feat(season): show the season deadline in the table and /season"
```

---

### Task 5: Вкладка «Актив» — кто зовёт играть

**Files:**
- Create: `internal/storage/activity.go`
- Create: `internal/storage/activity_test.go`
- Modify: `internal/bot/keyboards.go` (`statsTabs`, ~строка 216)
- Modify: `internal/bot/handlers_stats.go` (`statsView`, новый `case "activity"`)
- Modify: `internal/bot/messages.go` (новая `activityText`)
- Modify: `internal/bot/messages_test.go`

**Interfaces:**
- Consumes: предикаты `sqlSeasonalGames`/`sqlDuelGames`/`sqlCompletedGames`.
- Produces:
  - ```go
    type InitiativeRow struct {
        PlayerID   int64
        Name       string
        GamesAll   int    // season games this player started
        DuelsAll   int    // duels this player issued
        Games30    int    // same, last 30 days
        Duels30    int
        LastPlayed string // UTC "2006-01-02 15:04:05"; "" = never played
    }
    ```
  - `(*Store).InitiativeStats(chatID int64, since string) ([]InitiativeRow, error)` — `since` = UTC-граница окна 30 дней;
  - `activityText(rows []storage.InitiativeRow, now time.Time) string`.

- [ ] **Step 1: Write the failing storage test**

Создать `internal/storage/activity_test.go`:

```go
package storage

import "testing"

// TestInitiativeStats: games are attributed to their creator (by tg id), split
// into season games and duels, with a 30-day window and the last time each
// player actually played.
func TestInitiativeStats(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-5001)
	if err := st.EnsureChat(chat, 1); err != nil {
		t.Fatalf("EnsureChat: %v", err)
	}
	se, err := st.ActiveSeason(chat)
	if err != nil {
		t.Fatalf("ActiveSeason: %v", err)
	}
	a, _, _ := st.RegisterPlayer(chat, 11, "Alice")
	b, _, _ := st.RegisterPlayer(chat, 12, "Bob")

	// NOTE: the shared makeNormalGame/makeDuelGame helpers pass a *player* id as
	// created_by, but InitiativeStats matches created_by against players.tg_id.
	// Seed locally with real tg ids instead.
	seed := func(creatorTg int64, duel bool) {
		t.Helper()
		gid, err := st.CreatePendingGame(chat, se.ID, creatorTg, "medium", "hardcore")
		if err != nil {
			t.Fatalf("CreatePendingGame: %v", err)
		}
		if duel {
			if err := st.SetDuelTarget(gid, b.ID); err != nil {
				t.Fatalf("SetDuelTarget: %v", err)
			}
		}
		if err := st.AddPick(gid, a.ID); err != nil {
			t.Fatalf("AddPick a: %v", err)
		}
		if err := st.AddPick(gid, b.ID); err != nil {
			t.Fatalf("AddPick b: %v", err)
		}
		if err := st.FinalizeGame(gid, se.PointsTable); err != nil {
			t.Fatalf("FinalizeGame: %v", err)
		}
	}
	// Alice starts two season games and one duel; Bob starts nothing.
	seed(11, false)
	seed(11, false)
	seed(11, true)

	rows, err := st.InitiativeStats(chat, "1970-01-01 00:00:00")
	if err != nil {
		t.Fatalf("InitiativeStats: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2 (%+v)", len(rows), rows)
	}
	if rows[0].Name != "Alice" {
		t.Errorf("rows[0] = %s, want Alice first (most initiated)", rows[0].Name)
	}
	if rows[0].GamesAll != 2 || rows[0].DuelsAll != 1 {
		t.Errorf("Alice: games=%d duels=%d, want 2/1", rows[0].GamesAll, rows[0].DuelsAll)
	}
	if rows[0].Games30 != 2 || rows[0].Duels30 != 1 {
		t.Errorf("Alice 30d: games=%d duels=%d, want 2/1", rows[0].Games30, rows[0].Duels30)
	}
	if rows[1].GamesAll != 0 || rows[1].DuelsAll != 0 {
		t.Errorf("Bob: games=%d duels=%d, want 0/0", rows[1].GamesAll, rows[1].DuelsAll)
	}
	// Bob never started a game but did play in them.
	if rows[1].LastPlayed == "" {
		t.Errorf("Bob: LastPlayed empty, want the last game he played in")
	}

	// A window that excludes everything zeroes the 30-day counters but keeps
	// the all-time ones.
	rows, err = st.InitiativeStats(chat, "2099-01-01 00:00:00")
	if err != nil {
		t.Fatalf("InitiativeStats future window: %v", err)
	}
	if rows[0].GamesAll != 2 || rows[0].Games30 != 0 {
		t.Errorf("Alice future window: all=%d 30d=%d, want 2/0", rows[0].GamesAll, rows[0].Games30)
	}
}

// TestInitiativeStatsIgnoresUnknownCreator: games created by a tg id that maps
// to no player (legacy rows) are not attributed to anyone.
func TestInitiativeStatsIgnoresUnknownCreator(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-5002)
	if err := st.EnsureChat(chat, 1); err != nil {
		t.Fatalf("EnsureChat: %v", err)
	}
	se, _ := st.ActiveSeason(chat)
	a, _, _ := st.RegisterPlayer(chat, 11, "Alice")
	b, _, _ := st.RegisterPlayer(chat, 12, "Bob")

	gid, err := st.CreatePendingGame(chat, se.ID, 999, "medium", "hardcore") // stranger
	if err != nil {
		t.Fatalf("CreatePendingGame: %v", err)
	}
	if err := st.AddPick(gid, a.ID); err != nil {
		t.Fatalf("AddPick: %v", err)
	}
	if err := st.AddPick(gid, b.ID); err != nil {
		t.Fatalf("AddPick: %v", err)
	}
	if err := st.FinalizeGame(gid, se.PointsTable); err != nil {
		t.Fatalf("FinalizeGame: %v", err)
	}

	rows, err := st.InitiativeStats(chat, "1970-01-01 00:00:00")
	if err != nil {
		t.Fatalf("InitiativeStats: %v", err)
	}
	for _, r := range rows {
		if r.GamesAll != 0 || r.DuelsAll != 0 {
			t.Errorf("%s credited with a stranger's game: %+v", r.Name, r)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
docker run --rm -v "$PWD":/src -w /src -v gomodcache:/go/pkg/mod -v gobuildcache:/root/.cache/go-build \
  golang:1.25-alpine sh -c 'go test ./internal/storage/ -run InitiativeStats -v'
```

Expected: FAIL (build) — `st.InitiativeStats undefined`.

- [ ] **Step 3: Implement the query**

Создать `internal/storage/activity.go`:

```go
package storage

// InitiativeRow is one player's "who calls the games" record: how many rounds
// they started, all-time and in the recent window, plus when they last played.
type InitiativeRow struct {
	PlayerID   int64
	Name       string
	GamesAll   int    // season games this player started
	DuelsAll   int    // duels this player issued
	Games30    int    // season games started inside the window
	Duels30    int    // duels issued inside the window
	LastPlayed string // UTC "2006-01-02 15:04:05"; "" = never played
}

// InitiativeStats returns every active player's initiative record, most
// initiated first. since bounds the recent window (UTC datetime string).
// Games whose created_by matches no player (legacy rows imported before the
// column existed) are attributed to nobody.
func (s *Store) InitiativeStats(chatID int64, since string) ([]InitiativeRow, error) {
	rows, err := s.db.Query(`
		SELECT p.id, p.name,
		       (SELECT COUNT(*) FROM games g WHERE g.chat_id=p.chat_id
		          AND g.created_by=p.tg_id AND `+sqlSeasonalGames+`) AS games_all,
		       (SELECT COUNT(*) FROM games g WHERE g.chat_id=p.chat_id
		          AND g.created_by=p.tg_id AND `+sqlDuelGames+`) AS duels_all,
		       (SELECT COUNT(*) FROM games g WHERE g.chat_id=p.chat_id
		          AND g.created_by=p.tg_id AND `+sqlSeasonalGames+`
		          AND g.completed_at >= ?) AS games_30,
		       (SELECT COUNT(*) FROM games g WHERE g.chat_id=p.chat_id
		          AND g.created_by=p.tg_id AND `+sqlDuelGames+`
		          AND g.completed_at >= ?) AS duels_30,
		       COALESCE((SELECT MAX(g.completed_at) FROM game_results gr
		                 JOIN games g ON g.id = gr.game_id
		                 WHERE gr.player_id=p.id AND `+sqlCompletedGames+`), '') AS last_played
		FROM players p
		WHERE p.chat_id=? AND p.active=1
		ORDER BY (games_all + duels_all) DESC, p.name COLLATE NOCASE`,
		since, since, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []InitiativeRow
	for rows.Next() {
		var r InitiativeRow
		if err := rows.Scan(&r.PlayerID, &r.Name, &r.GamesAll, &r.DuelsAll,
			&r.Games30, &r.Duels30, &r.LastPlayed); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
```

- [ ] **Step 4: Run the storage tests**

```bash
docker run --rm -v "$PWD":/src -w /src -v gomodcache:/go/pkg/mod -v gobuildcache:/root/.cache/go-build \
  golang:1.25-alpine sh -c 'go test ./internal/storage/ -run InitiativeStats -v && go test ./internal/storage/'
```

Expected: оба новых теста PASS, пакет `ok`.

- [ ] **Step 5: Write the failing render test**

Дописать в конец `internal/bot/messages_test.go`:

```go
func TestActivityText(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	rows := []storage.InitiativeRow{
		{Name: "Joe Rude", GamesAll: 47, DuelsAll: 39, Games30: 12, Duels30: 8,
			LastPlayed: "2026-07-20 08:00:00"},
		{Name: "mister", GamesAll: 14, DuelsAll: 7, Games30: 0, Duels30: 0,
			LastPlayed: "2026-07-14 08:00:00"},
		{Name: "Ghost", LastPlayed: ""},
	}
	got := activityText(rows, now)

	for _, want := range []string{
		"Актив",
		"Joe Rude",
		"47", "39", "12",
		"сегодня",
		"mister",
		"6 дн",
		"Ghost",
		"ещё не играл",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("activityText missing %q in:\n%s", want, got)
		}
	}
}

func TestActivityTextEmpty(t *testing.T) {
	got := activityText(nil, time.Now())
	if !strings.Contains(got, "/join") {
		t.Errorf("empty roster should hint at /join, got:\n%s", got)
	}
}
```

- [ ] **Step 6: Run test to verify it fails**

```bash
docker run --rm -v "$PWD":/src -w /src -v gomodcache:/go/pkg/mod -v gobuildcache:/root/.cache/go-build \
  golang:1.25-alpine sh -c 'go test ./internal/bot/ -run ActivityText -v'
```

Expected: FAIL (build) — `undefined: activityText`.

- [ ] **Step 7: Implement the renderer**

В `internal/bot/messages.go` в конец файла добавить:

```go
// activityText renders the "who calls the games" tab: rounds started per
// player (all-time and last 30 days) and how long since they last played.
func activityText(rows []storage.InitiativeRow, now time.Time) string {
	var b strings.Builder
	b.WriteString("🎯 <b>Актив</b> · кто зовёт играть\n\n")
	if len(rows) == 0 {
		b.WriteString("Пока нет игроков. /join")
		return b.String()
	}
	for i, r := range rows {
		fmt.Fprintf(&b, "%s <b>%s</b> — <b>%d</b> игр · <b>%d</b> дуэлей\n",
			medal(i+1), esc(r.Name), r.GamesAll, r.DuelsAll)
		fmt.Fprintf(&b, "   <i>за 30 дн: %d · %d · %s</i>\n",
			r.Games30, r.Duels30, lastPlayedPhrase(r.LastPlayed, now))
	}
	b.WriteString("\n<i>Считаются только созданные игры: /play, /duel, /invite.</i>")
	return b.String()
}

// lastPlayedPhrase renders how long ago a player last played a recorded game.
func lastPlayedPhrase(lastPlayed string, now time.Time) string {
	if lastPlayed == "" {
		return "ещё не играл"
	}
	t, err := parseDBTime(lastPlayed)
	if err != nil {
		return "ещё не играл"
	}
	switch days := int(now.UTC().Sub(t).Hours() / 24); {
	case days <= 0:
		return "играл сегодня"
	case days == 1:
		return "не играл 1 дн"
	default:
		return fmt.Sprintf("не играл %d дн", days)
	}
}
```

- [ ] **Step 8: Run test to verify it passes**

```bash
docker run --rm -v "$PWD":/src -w /src -v gomodcache:/go/pkg/mod -v gobuildcache:/root/.cache/go-build \
  golang:1.25-alpine sh -c 'go test ./internal/bot/ -run ActivityText -v'
```

Expected: PASS (2 теста).

- [ ] **Step 9: Add the tab**

В `internal/bot/keyboards.go` в `statsTabs` добавить последним элементом:

```go
	{"activity", "🎯 Актив"},
```

В `internal/bot/handlers_stats.go` в `statsView` перед `default:` добавить ветку:

```go
	case "activity":
		since := time.Now().UTC().AddDate(0, 0, -30).Format("2006-01-02 15:04:05")
		init, e := b.st.InitiativeStats(chatID, since)
		if e != nil {
			return "", nil, e
		}
		text = activityText(init, time.Now())
```

- [ ] **Step 10: Build, vet, full test run**

```bash
docker run --rm -v "$PWD":/src -w /src -v gomodcache:/go/pkg/mod -v gobuildcache:/root/.cache/go-build \
  golang:1.25-alpine sh -c 'go build ./... && go vet ./... && go test ./...'
```

Expected: всё зелёное.

- [ ] **Step 11: Commit**

```bash
git add internal/storage/activity.go internal/storage/activity_test.go \
        internal/bot/keyboards.go internal/bot/handlers_stats.go \
        internal/bot/messages.go internal/bot/messages_test.go
git commit -m "feat(stats): «Актив» tab — who calls the games and who went quiet"
```

---

### Task 6: Деплой и проверка на проде

**Files:** нет (операционная задача).

**Interfaces:**
- Consumes: собранный бинарь со всеми задачами выше.
- Produces: работающий прод + проставленный дедлайн у сезона 5.

- [ ] **Step 1: Back up the production database**

```bash
docker run --rm -v "$PWD/data":/data golang:1.25-alpine \
  sh -c 'apk add -q sqlite && sqlite3 /data/sudoku.db ".backup /data/db.bak-20260720-seasons"'
```

Expected: команда без вывода, файл появляется в `data/`.

- [ ] **Step 2: Deploy**

```bash
docker compose up -d --build --force-recreate
```

Expected: `Container sudoku-bot  Started`. Если новый код не подхватился (см. CLAUDE.md про залипший кэш) — `docker compose build --no-cache bot`, затем повторить.

- [ ] **Step 3: Verify the fresh binary and a clean start**

```bash
docker compose exec -T bot sh -c 'strings /app/sudoku-bot | grep -a "InitiativeStats" | head -2'
docker compose logs bot | tail -5
```

Expected: символ `InitiativeStats` присутствует; в логах `bot started`.
(Грепать ASCII-символ, не кириллицу — `strings` её вырезает.)

- [ ] **Step 4: Verify the deadline was assigned within a minute**

```bash
sleep 70
docker run --rm -v "$PWD/data":/data:ro golang:1.25-alpine \
  sh -c 'apk add -q sqlite && sqlite3 -header -column "file:/data/sudoku.db?mode=ro" \
  "SELECT id, chat_id, number, status, deadline FROM seasons WHERE status='"'"'active'"'"'"'
```

Expected: у всех трёх активных сезонов заполнен `deadline` = `2026-07-31 18:00:00`
(1 августа 00:00 по Asia/Bishkek). Мёртвым чатам бот ничего не пишет — у них 0 очков,
сработает тихое продление.

- [ ] **Step 5: Verify the UI in Telegram**

В группе выполнить `/stats` → вкладка «🎯 Актив» рисуется, в «🏆 Таблица» внизу
строка «📅 Финал сезона: 31 июля (через N дн)». Проверить `/season`.

- [ ] **Step 6: Commit any fixes found during verification**

Если что-то поправлено — коммит в стиле репо (Conventional Commits, без трейлера).

---

## Готово

После Task 6 сезон 5 закроется автоматически 1 августа 00:00 по Бишкеку (или раньше, если кто-то доберётся до 100 очков), победителем станет лидер таблицы, и сразу стартует сезон 6 с дедлайном 1 сентября.
