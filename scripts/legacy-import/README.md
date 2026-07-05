# Импорт легаси-сезонов 1-3 (Google Sheets → БД бота)

`import_legacy_seasons.sql` переносит 222 игры трёх досезонных таблиц
(окт 2025 - янв 2026) в прод-БД. Сгенерирован `generate.py` из CSV-экспортов
листов «Usdoku Table Standings Season I/II/III» 2026-07-05.

## Что делает

- Текущий активный сезон перенумеровывается в **4** (только поле `number`).
- Вставляются сезоны 1-3 со статусом `archived`, победителями
  (S1 Nur 93, S2 Nur 100, S3 mister 101) и датами начала/конца.
- 222 игры + 535 строк результатов. Отрицательные id (`seasons` -1..-3,
  `games` -222..-1) хронологичны и не пересекаются с автоинкрементом.
- 108 реальных времён решений из Season I идут в `duration_secs`.
- Всё в одной транзакции; в конце файла блок VERIFICATION из 10 SELECT,
  где `actual` обязан равняться `expected`.

## Зафиксированные решения по данным

- Маппинг: Zhoomart → Joe Rude, Kairat → Nur, Batyrhan → mister.
- `difficulty='medium'` всем играм (времена S1 участвуют в /speed и /records).
- 27 строк Season II с датой 7/5/2026 (артефакт правки листа) и 47 игр S1
  без даты наследуют дату предыдущей строки листа.
- Синтетическое время: 07:00 UTC + 3 мин на игру внутри дня (в Бишкеке
  рендерится как день игры, 13:00+). Порядок игр = порядок листа.
- Игры, где записана часть участников, импортируются как есть: ранги по
  порядку строк, очки дословно из листа. DNF-строки (rank 0) не выдумываются.

## Повторный запуск

Защищено: вторая попытка падает на `UNIQUE constraint failed: seasons.id`,
и транзакция откатывается целиком. База не портится.

## Как накатить (на VM с прод-БД)

```sh
cd ~/sudoku-bot-telegram
# 1. WAL-safe бэкап
docker run --rm -v "$PWD/data":/data alpine sh -c \
  'apk add -q sqlite && sqlite3 /data/sudoku.db ".backup /data/sudoku.db.bak-pre-legacy"'
# 2. Прогон на копии + верификация (actual == expected во всех 10 строках)
docker run --rm -v "$PWD/data":/data -v "$PWD/scripts/legacy-import/import_legacy_seasons.sql":/q.sql alpine sh -c \
  'apk add -q sqlite && cp /data/sudoku.db /tmp/t.db && sqlite3 /tmp/t.db < /q.sql'
# 3. Если всё OK - на прод
docker run --rm -v "$PWD/data":/data -v "$PWD/scripts/legacy-import/import_legacy_seasons.sql":/q.sql alpine sh -c \
  'apk add -q sqlite && sqlite3 /data/sudoku.db < /q.sql'
```

Проверка в Telegram после наката: `/history all` (легаси-игры внизу),
`/me` (карьерные бейджи 🏅💪💯🎯 пересчитаются сами), `/stats` → Рекорды
(исторические лучшие времена из Season I).

Откат: остановить бот, вернуть `sudoku.db.bak-pre-legacy` на место
(`.restore`), запустить бот.
