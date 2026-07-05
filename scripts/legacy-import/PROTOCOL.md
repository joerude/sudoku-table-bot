# Протокол наката легаси-сезонов на прод

Цель: перенести сезоны 1-3 из Google Sheets в прод-БД бота.
Время: ~10 минут. Бот останавливать не нужно.
Все команды выполняются на VM, из корня репозитория (`cd ~/sudoku-bot-telegram`).

---

## 0. Предусловия

- [ ] Ветка `cleanup/redundancy-and-messages` запушена с ноутбука
- [ ] На VM: `git pull` (или checkout ветки)
- [ ] Файл на месте: `ls scripts/legacy-import/import_legacy_seasons.sql` → 780 строк

## 1. Бэкап (обязательно)

```sh
docker run --rm -v "$PWD/data":/data alpine sh -c \
  'apk add -q sqlite && sqlite3 /data/sudoku.db ".backup /data/sudoku.db.bak-pre-legacy"'
```

- [ ] Файл появился: `ls -la data/sudoku.db.bak-pre-legacy`

## 2. Репетиция на копии (прод не трогается)

```sh
docker run --rm -v "$PWD/data":/data \
  -v "$PWD/scripts/legacy-import/import_legacy_seasons.sql":/q.sql alpine sh -c \
  'apk add -q sqlite && cp /data/sudoku.db /tmp/t.db && sqlite3 /tmp/t.db < /q.sql'
```

- [ ] В выводе 10 строк проверки, везде `actual` == `expected`:

| строка | expected (pts/wins/games) |
|---|---|
| games total | 222 |
| s1 Joe Rude / Nur / mister | 69/22/63 · 93/33/63 · 70/29/63 |
| s2 Joe Rude / Nur / mister | 70/16/60 · 100/27/58 · 80/22/57 |
| s3 Joe Rude / Nur / mister | 81/23/56 · 89/21/59 · 101/29/56 |

**СТОП-условие:** любое расхождение или ошибка → не идти дальше, прислать вывод Клоду.

## 3. Накат на прод

```sh
docker run --rm -v "$PWD/data":/data \
  -v "$PWD/scripts/legacy-import/import_legacy_seasons.sql":/q.sql alpine sh -c \
  'apk add -q sqlite && sqlite3 /data/sudoku.db < /q.sql'
```

- [ ] Те же 10 строк проверки, везде совпадение

## 4. Проверка в Telegram

- [ ] `/history all` - легаси-игры видны (внизу списка, старые даты)
- [ ] `/me` - выросли «Игр»/«Побед», карьерные бейджи пересчитались (🏅 у Nur и mister)
- [ ] `/stats` → Рекорды - времена из сезона I (лучшее ~3:48 Nur)
- [ ] `/status` - текущий сезон называется «Сезон 4», очки в нём НЕ изменились

## 5. Откат (только при проблемах)

```sh
docker compose stop bot
docker run --rm -v "$PWD/data":/data alpine sh -c \
  'apk add -q sqlite && sqlite3 /data/sudoku.db ".restore /data/sudoku.db.bak-pre-legacy"'
docker compose start bot
docker compose logs bot | tail   # ждать "bot started"
```

---

## FAQ

**Запустил шаг 3 дважды.** Не страшно: вторая попытка падает на
`UNIQUE constraint failed: seasons.id`, транзакция откатывается целиком.

**Во время наката шла игра.** Не страшно: SQLite сериализует записи,
скрипт работает одной короткой транзакцией.

**Имена игроков в БД отличаются от Joe Rude / Nur / mister.**
Скрипт упадёт сам (NOT NULL на player_id) и ничего не запишет.
Прислать Клоду вывод `SELECT id, name FROM players`.
