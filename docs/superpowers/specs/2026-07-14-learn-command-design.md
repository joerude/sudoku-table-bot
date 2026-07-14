# /learn — справочник техник судоку

Дата: 2026-07-14
Статус: утверждён (brainstorming)

## Задача

В чате лиги нет ни одного обучающего материала: игроки упираются в hard/expert,
делают DNF и не знают, какую технику учить дальше. Нужен встроенный справочник:
открыл `/learn`, выбрал уровень, выбрал технику — получил короткое объяснение
по-русски и ссылку на подробную статью.

Не цель: обучение с прогрессом, тесты, ежедневные пуши, разбор конкретной доски.

## Решение

Статичный словарь из 18 техник, разбитый на 3 уровня по 6. Ноль состояния, ноль
таблиц в БД, ноль сети во время запроса. Навигация — инлайн-кнопки, сообщение
трансформируется через `c.Edit` (конвенция пикеров в этом боте).

### Данные — `internal/domain/learn.go`

Чистый пакет, без зависимостей от telebot и storage:

```go
type Tier string

const (
    TierBasic Tier = "basic" // Базовые
    TierMid   Tier = "mid"   // Средние
    TierAdv   Tier = "adv"   // Продвинутые
)

type Technique struct {
    Key   string // короткий ascii-ключ, безопасен для callback data: "x-wing"
    Name  string // RU: "X-Wing"
    Alias string // EN-название — что гуглить
    Tier  Tier
    Blurb string // RU, 2-4 предложения: когда применять и что даёт
    URL   string // основная статья (sudoku.com или sudokuwiki)
    Wiki  string // опционально: глубокая статья на sudokuwiki.org
}

func Techniques() []Technique              // все 18, порядок фиксирован
func TechniquesByTier(t Tier) []Technique  // 6 штук
func TechniqueByKey(k string) (Technique, bool)
func RandomTechnique(r *rand.Rand) Technique
```

`rand.Rand` передаётся снаружи — чтобы тест был детерминированным.

### Содержимое (ссылки проверены 2026-07-14)

**Базовые** (sudoku.com/sudoku-rules):
1. `obvious-single` Голая одиночка — /obvious-singles/
2. `hidden-single` Скрытая одиночка — /hidden-singles/
3. `obvious-pair` Голая пара — /obvious-pairs/
4. `hidden-pair` Скрытая пара — /hidden-pairs/
5. `pointing-pair` Указывающая пара — /pointing-pairs/ (wiki: intersection_removal)
6. `pointing-triple` Указывающая тройка — /pointing-triples/ (wiki: intersection_removal)

**Средние**:
7. `obvious-triple` Голая тройка — /obvious-triples/
8. `hidden-triple` Скрытая тройка — /hidden-triples/
9. `box-line` Блок-линия — sudokuwiki.org/intersection_removal (на sudoku.com статьи нет)
10. `x-wing` X-Wing — /h-wing/ **(да, слаг такой: опечатка на самом sudoku.com)** (wiki: x_wing_strategy)
11. `y-wing` Y-Wing — /y-wing/ (wiki: y_wing_strategy)
12. `swordfish` Swordfish — /swordfish/

**Продвинутые** (только sudokuwiki.org — на sudoku.com этих техник нет):
13. `xyz-wing` XYZ-Wing — /XYZ_Wing
14. `jellyfish` Jellyfish — /Jelly_Fish_Strategy
15. `simple-coloring` Простая раскраска — /Simple_Colouring
16. `singles-chain` Цепочки одиночек — /Singles_Chains
17. `xy-chain` XY-Chains — /XY_Chains
18. `unique-rect` Уникальный прямоугольник — /Unique_Rectangles

Правило: `URL` всегда непустой и https. `Wiki` заполняется только если ссылка
проверена и отличается от `URL`.

### UI

```
/learn  →  📚 Техники судоку
           [Базовые] [Средние] [Продвинутые]
           [🎲 Случайная]

tier    →  🧩 Средние
           [Голая тройка] [Скрытая тройка] ...6 кнопок...
           [⬅️ Назад]

tech    →  <b>X-Wing</b> · X-Wing
           <i>Средние</i>

           Блурб на 3 предложения.

           [🔗 Разбор] [📖 sudokuwiki]   ← вторая кнопка только если Wiki != ""
           [⬅️ Назад]                     ← возврат в свой tier
```

Всё через `c.Edit` — одно сообщение трансформируется, чат не засоряется.
Сообщение НЕ эфемерное (справочник — ценный контент, пусть висит).
Весь текст — через `esc()`.

### Файлы

- `internal/domain/learn.go` + `learn_test.go` — словарь (новый)
- `internal/bot/handlers_learn.go` — `onLearn`, `onLearnTier`, `onLearnTech`, `onLearnRandom` (новый)
- `internal/bot/messages.go` — `learnRootMsg()`, `learnTierMsg(tier)`, `learnTechMsg(t)`
- `internal/bot/keyboards.go` — `learnRootKeyboard()`, `learnTierKeyboard(tier)`, `learnTechKeyboard(t)`
- `internal/bot/bot.go` — `cbLearnTier/cbLearnTech/cbLearnRand/cbLearnRoot` + роуты
- `internal/bot/handlers_basic.go` — строка в `/help`

### Ограничения и риски

- **Callback data ≤ 64 байт** (лимит Telegram). Ключи — короткий ascii, самый
  длинный `simple-coloring` (15) → с префиксом уникального колбэка влезает с запасом.
  Тест это проверяет.
- **Внешние ссылки могут протухнуть.** Тест сети не бьёт (в docker без интернета),
  проверяет только формат (https, непустой). Протухание ловим глазами.
- Тексты блурбов — ручные, ревью на фактическую корректность обязателен.

### Тесты

`internal/domain/learn_test.go`:
- ровно 18 техник, по 6 в каждом уровне
- ключи уникальны, ascii, `len(key) <= 24`
- `Name`, `Alias`, `Blurb`, `URL` непустые; URL начинается с `https://`
- `TechniqueByKey` находит каждый ключ и возвращает `false` на мусоре
- `RandomTechnique` с фиксированным seed детерминирована и возвращает элемент словаря

`internal/bot/keyboards_test.go` / `messages_test.go`:
- корневая клавиатура: 3 tier-кнопки + случайная
- tier-клавиатура: 6 кнопок + Назад; callback data ≤ 64 байт
- клавиатура техники: ссылка присутствует; кнопка wiki только при `Wiki != ""`
- `learnTechMsg` содержит имя, алиас, блурб; экранирует HTML

### Явно НЕ делаем

- кнопку в `/play`-хабе (хаб про «во что играем», не про учёбу)
- подсказку после DNF (лезет в рендер игровых постов и их тесты)
- ежедневный «совет дня» (крон, шум в чате)
- таблицу в БД, прогресс изучения, поиск по словарю
