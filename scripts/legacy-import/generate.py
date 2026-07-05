#!/usr/bin/env python3
"""Generate import_legacy_seasons.sql from the three Google-Sheets CSV exports.

Data decisions (confirmed by Zhoomart, 2026-07-05):
  Zhoomart -> Joe Rude, Kairat -> Nur, Batyrhan -> mister
  S2 game ZFVN mis-dated 7/5/2026 -> 11/17/2025 (by sheet position)
  difficulty = 'medium' for all legacy games
  Missing dates inherit the previous game's date (sheet order is chronological).
  Synthetic times: 07:00 UTC + 3 min per game within a day (renders 13:00+ Bishkek).
  Negative ids (seasons -1..-3, games -222..-1) double as an idempotency guard:
  a second run hits a PK conflict and the whole transaction rolls back.
"""
import csv, re, collections, datetime

MAP = {'Zhoomart': 'Joe Rude', 'Kairat': 'Nur', 'Batyrhan': 'mister'}
CHAT = "(SELECT chat_id FROM chats LIMIT 1)"

def pid(sheet_name):
    return f"(SELECT id FROM players WHERE chat_id={CHAT} AND name='{MAP[sheet_name]}')"

def parse_s1(path):
    rows = list(csv.reader(open(path, encoding='utf-8-sig')))[1:]
    games, cur, prev_gid = [], None, object()
    for r in rows:
        r += [''] * (11 - len(r))
        gid, player, tm, place, pts, note, date = [x.strip() for x in r[:7]]
        if not player:
            prev_gid = object()
            continue
        if gid != prev_gid or cur is None:
            cur = {'id': (None if gid in ('', '-') else gid), 'date': None, 'res': []}
            games.append(cur)
            prev_gid = gid
        if date:
            d, m, y = date.split('.')
            cur['date'] = datetime.date(int(y), int(m), int(d))
        secs = None
        m2 = re.fullmatch(r'(\d+):(\d\d)', tm)
        if m2:
            secs = int(m2.group(1)) * 60 + int(m2.group(2))
        rank = int(place) if place.isdigit() else len(cur['res']) + 1
        cur['res'].append((player, rank, int(pts) if pts.isdigit() else 0, secs))
    return games

def parse_wide(path, valid=None):
    games = []
    for r in csv.DictReader(open(path, encoding='utf-8-sig')):
        gid = (r.get('ID') or '').strip() or None
        ds = (r.get('Дата') or '').strip()
        places = [(r.get(k) or '').strip() for k in ('1st', '2nd', '3rd')]
        if not any(places):
            continue
        date = None
        if ds:
            m, d, y = ds.split('/')
            date = datetime.date(int(y), int(m), int(d))
            if valid and not (valid[0] <= date <= valid[1]):
                date = None
        pts = {'Zhoomart': r.get('Zhoomart PTS', ''), 'Kairat': r.get('Kairat PTS', ''),
               'Batyrhan': r.get('Batyr PTS', '')}
        res = [(name, rank, int((pts[name] or '0').strip() or 0), None)
               for rank, name in enumerate(places, 1) if name]
        games.append({'id': gid, 'date': date, 'res': res})
    return games

def fill_dates(games):
    last = next(g['date'] for g in games if g['date'])
    for g in games:
        if g['date'] is None:
            g['date'] = last
        last = g['date']
    return games

s1 = fill_dates(parse_s1('/tmp/s1.csv'))
s2 = fill_dates(parse_wide('/tmp/s2.csv', valid=(datetime.date(2025,11,1), datetime.date(2025,12,10))))
s3 = fill_dates(parse_wide('/tmp/s3.csv', valid=(datetime.date(2025,12,1), datetime.date(2026,1,31))))
seasons = [(1, s1), (2, s2), (3, s3)]

def esc(s):
    return s.replace("'", "''")

total = sum(len(g) for _, g in seasons)
out, gid_counter = [], -total
out.append("-- Legacy seasons 1-3 import (Google Sheets -> bot DB). Generated 2026-07-05.")
out.append("-- Run AFTER a WAL-safe backup. Single transaction: any error rolls everything back.")
out.append("BEGIN;")
out.append(f"UPDATE seasons SET number=4 WHERE status='active' AND chat_id={CHAT};")

expected = []
for num, games in seasons:
    first, last = games[0]['date'], games[-1]['date']
    out.append(f"INSERT INTO seasons(id, chat_id, number, target, points_table, status, winner_id, started_at, ended_at)")
    tot = collections.Counter()
    for g in games:
        for p, rank, pts, _ in g['res']:
            tot[p] += pts
    winner = max(tot, key=lambda p: tot[p])
    out.append(f"VALUES(-{num}, {CHAT}, {num}, 100, '[3,1,0]', 'archived', {pid(winner)}, "
               f"'{first} 07:00:00', '{last} 20:00:00');")
    wins = collections.Counter(); played = collections.Counter()
    for g in games:
        for p, rank, pts, _ in g['res']:
            played[p] += 1
            if rank == 1:
                wins[p] += 1
    for p in MAP:
        expected.append((num, p, tot[p], wins[p], played[p]))

for num, games in seasons:
    per_day = collections.Counter()
    for g in games:
        minute = per_day[g['date']] * 3
        per_day[g['date']] += 1
        ts = f"{g['date']} {7 + minute // 60:02d}:{minute % 60:02d}:00"
        code = f"'{esc(g['id'])}'" if g['id'] else 'NULL'
        out.append(f"INSERT INTO games(id, chat_id, season_id, status, difficulty, mode, usdoku_code, "
                   f"reminded, deleted, created_by, created_at, completed_at) "
                   f"VALUES({gid_counter}, {CHAT}, -{num}, 'completed', 'medium', NULL, {code}, "
                   f"1, 0, NULL, '{ts}', '{ts}');")
        for p, rank, pts, secs in g['res']:
            out.append(f"INSERT INTO game_results(game_id, player_id, rank, points, duration_secs) "
                       f"VALUES({gid_counter}, {pid(p)}, {rank}, {pts}, {secs if secs else 'NULL'});")
        gid_counter += 1
out.append("COMMIT;")

out.append("\n-- ===== VERIFICATION (expected vs actual; diff column must be all-zero) =====")
out.append("SELECT 'games total' k, (SELECT COUNT(*) FROM games WHERE id < 0) actual, %d expected;" % total)
for num, name, pts, wins, played in expected:
    out.append(
        f"SELECT 's{num} {MAP[name]}' k, COALESCE(SUM(gr.points),0)||'/'||SUM(gr.rank=1)||'/'||COUNT(*) actual, "
        f"'{pts}/{wins}/{played}' expected FROM game_results gr JOIN games g ON g.id=gr.game_id "
        f"WHERE g.season_id=-{num} AND gr.player_id={pid(name)};"
    )
open('/tmp/gen/import_legacy_seasons.sql', 'w').write('\n'.join(out) + '\n')
print(f"generated: {total} games, {sum(1 for l in out if 'game_results' in l)} result rows")
