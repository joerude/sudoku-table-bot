CREATE TABLE IF NOT EXISTS chats (
    chat_id        INTEGER PRIMARY KEY,
    admin_id       INTEGER,
    tz             TEXT    NOT NULL DEFAULT 'Asia/Bishkek',
    daily_reminder INTEGER NOT NULL DEFAULT 1,        -- bool: send daily nudge
    daily_time     TEXT    NOT NULL DEFAULT '21:00',  -- HH:MM local
    min_players    INTEGER NOT NULL DEFAULT 2,        -- min participants for a game to count
    last_daily     TEXT,                              -- date (YYYY-MM-DD) of last daily nudge
    weekly_digest  INTEGER NOT NULL DEFAULT 1,        -- bool: send weekly digest
    last_weekly    TEXT,                              -- date (YYYY-MM-DD) of last digest
    created_at     TEXT    NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS players (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    chat_id     INTEGER NOT NULL,
    tg_id       INTEGER,                       -- Telegram user id (nullable)
    name        TEXT    NOT NULL,
    usdoku_nick TEXT,                          -- nickname used on usdoku (for auto-record)
    active      INTEGER NOT NULL DEFAULT 1,
    created_at  TEXT    NOT NULL DEFAULT (datetime('now')),
    UNIQUE(chat_id, tg_id)
);

CREATE TABLE IF NOT EXISTS seasons (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    chat_id      INTEGER NOT NULL,
    number       INTEGER NOT NULL,
    target       INTEGER NOT NULL DEFAULT 100,
    points_table TEXT    NOT NULL DEFAULT '[3,1,0]',  -- JSON array of ints
    status       TEXT    NOT NULL DEFAULT 'active',    -- active | archived
    winner_id    INTEGER,
    started_at   TEXT    NOT NULL DEFAULT (datetime('now')),
    ended_at     TEXT
);

CREATE TABLE IF NOT EXISTS games (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    chat_id      INTEGER NOT NULL,
    season_id    INTEGER NOT NULL,
    status       TEXT    NOT NULL DEFAULT 'pending',   -- pending | completed
    difficulty   TEXT,
    mode         TEXT,
    usdoku_code  TEXT,
    reminded     INTEGER NOT NULL DEFAULT 0,           -- bool: event reminder sent
    deleted      INTEGER NOT NULL DEFAULT 0,           -- bool: soft-deleted (restorable)
    created_by   INTEGER,
    created_at   TEXT    NOT NULL DEFAULT (datetime('now')),
    completed_at TEXT
);

CREATE TABLE IF NOT EXISTS game_results (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    game_id   INTEGER NOT NULL,
    player_id INTEGER NOT NULL,
    rank      INTEGER NOT NULL,   -- 1-based finishing position
    points    INTEGER NOT NULL DEFAULT 0,
    duration_secs INTEGER,        -- solve time in seconds (auto-record only; NULL = unknown)
    UNIQUE(game_id, player_id)
);

CREATE INDEX IF NOT EXISTS idx_results_game ON game_results(game_id);
CREATE INDEX IF NOT EXISTS idx_games_season ON games(season_id, status);

CREATE TABLE IF NOT EXISTS game_rsvp (
    game_id   INTEGER NOT NULL,
    player_id INTEGER NOT NULL,
    PRIMARY KEY (game_id, player_id)
);
