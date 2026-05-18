-- Full database schema. Applied once on first start (idempotent).

-- ─── App settings ─────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS app_settings (
    key        VARCHAR(100) PRIMARY KEY,
    value      TEXT         NOT NULL,
    updated_at TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- ─── Users ────────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS users (
    id                    BIGSERIAL    PRIMARY KEY,
    username              VARCHAR(50)  UNIQUE NOT NULL,
    password_hash         VARCHAR(255) NOT NULL,
    role                  VARCHAR(20)  NOT NULL DEFAULT 'simple',
    is_admin              BOOLEAN      NOT NULL DEFAULT false,
    totp_secret           VARCHAR(64),
    totp_enabled          BOOLEAN      NOT NULL DEFAULT false,
    backup_codes          TEXT,
    premium_until         TIMESTAMPTZ,
    timecode_grace_until  TIMESTAMPTZ,
    notify_premium_after  TIMESTAMPTZ,
    notify_type           VARCHAR(20),
    premium_warned        BOOLEAN      NOT NULL DEFAULT false,
    timezone              VARCHAR(50),
    notify_start          INT          NOT NULL DEFAULT 9,
    notify_end            INT          NOT NULL DEFAULT 22,
    notifications_enabled BOOLEAN      NOT NULL DEFAULT true,
    last_active_at        DATE,
    inactive_warned       BOOLEAN      NOT NULL DEFAULT false,
    notify_inactive_after TIMESTAMPTZ,
    blocked_at            TIMESTAMPTZ,
    block_reason          VARCHAR(500),
    created_at            TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_users_username ON users (username);

-- ─── Sessions ─────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS sessions (
    id         BIGSERIAL    PRIMARY KEY,
    user_id    BIGINT       NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    key        VARCHAR(64)  UNIQUE NOT NULL,
    expires_at TIMESTAMPTZ  NOT NULL,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    ip         VARCHAR(50),
    user_agent VARCHAR(500)
);

CREATE INDEX IF NOT EXISTS idx_sessions_key     ON sessions (key);
CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions (user_id);

-- ─── Trusted devices (remember-me) ────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS trusted_devices (
    id           BIGSERIAL   PRIMARY KEY,
    user_id      BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token        VARCHAR(64) UNIQUE NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_trusted_devices_user_id ON trusted_devices (user_id);

-- ─── TOTP pending ─────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS totp_2fa_pending (
    id         BIGSERIAL   PRIMARY KEY,
    user_id    BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token      VARCHAR(64) UNIQUE NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ─── Password reset ───────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS password_reset_tokens (
    id         BIGSERIAL   PRIMARY KEY,
    user_id    BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token      VARCHAR(64) UNIQUE NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ─── Telegram ─────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS telegram_users (
    id          BIGSERIAL    PRIMARY KEY,
    user_id     BIGINT       UNIQUE NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    telegram_id BIGINT       UNIQUE NOT NULL,
    username    VARCHAR(100),
    linked_at   TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_telegram_users_telegram_id ON telegram_users (telegram_id);

CREATE TABLE IF NOT EXISTS telegram_link_codes (
    id         BIGSERIAL   PRIMARY KEY,
    user_id    BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code       VARCHAR(6)  UNIQUE NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS support_messages (
    id                BIGSERIAL   PRIMARY KEY,
    user_telegram_id  BIGINT      NOT NULL,
    user_username     VARCHAR(100),
    direction         VARCHAR(3)  NOT NULL,
    text              TEXT        NOT NULL,
    admin_telegram_id BIGINT,
    admin_msg_id      INT,
    is_read           BOOLEAN     NOT NULL DEFAULT false,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_support_messages_user_telegram_id  ON support_messages (user_telegram_id);
CREATE INDEX IF NOT EXISTS idx_support_messages_admin_telegram_id ON support_messages (admin_telegram_id);

-- ─── Devices ──────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS devices (
    id         BIGSERIAL    PRIMARY KEY,
    user_id    BIGINT       NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name       VARCHAR(100) NOT NULL DEFAULT 'Основное',
    token      VARCHAR(64)  UNIQUE NOT NULL,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_devices_user_id ON devices (user_id);
CREATE INDEX IF NOT EXISTS idx_devices_token   ON devices (token);

-- ─── Device codes ─────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS device_codes (
    id         BIGSERIAL   PRIMARY KEY,
    code       VARCHAR(6)  UNIQUE NOT NULL,
    user_id    BIGINT      REFERENCES users(id) ON DELETE CASCADE,
    device_id  BIGINT      REFERENCES devices(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_device_codes_code ON device_codes (code);

-- ─── Profiles ───────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS profiles (
    id               BIGSERIAL    PRIMARY KEY,
    device_id        BIGINT       NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    profile_id VARCHAR(100) NOT NULL,
    name             VARCHAR(100) NOT NULL DEFAULT '',
    icon             VARCHAR(20),
    favorite         TEXT,
    child            BOOLEAN      NOT NULL DEFAULT false,
    params           JSONB        NOT NULL DEFAULT '{}',
    CONSTRAINT uq_profile UNIQUE (device_id, profile_id)
);

CREATE INDEX IF NOT EXISTS idx_profiles_device_id ON profiles (device_id);

-- ─── Plugin settings ──────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS plugin_settings (
    user_id          BIGINT       NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    profile_id VARCHAR(100) NOT NULL DEFAULT '',
    plugin           VARCHAR(100) NOT NULL,
    settings         TEXT         NOT NULL DEFAULT '{}',
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, profile_id, plugin)
);

-- ─── Timecodes ────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS timecodes (
    id               BIGSERIAL    PRIMARY KEY,
    device_id        BIGINT       NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    profile_id VARCHAR(100) NOT NULL DEFAULT '',
    card_id          VARCHAR(100) NOT NULL,
    item             VARCHAR(100) NOT NULL,
    data             TEXT         NOT NULL,
    counted_at       DATE,
    view_count       INT          NOT NULL DEFAULT 0,
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT uq_timecode_unique UNIQUE (device_id, profile_id, card_id, item)
);

CREATE INDEX IF NOT EXISTS idx_timecodes_device_id ON timecodes (device_id);
CREATE INDEX IF NOT EXISTS idx_timecodes_card_id   ON timecodes (card_id);

-- ─── Media cards ──────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS media_cards (
    card_id              VARCHAR(100) PRIMARY KEY,
    tmdb_id              BIGINT       NOT NULL,
    media_type           VARCHAR(10)  NOT NULL,
    title                VARCHAR(500),
    original_title       VARCHAR(500),
    overview             TEXT,
    poster_path          VARCHAR(300),
    backdrop_path        VARCHAR(300),
    release_date         DATE,
    first_air_date       DATE,
    last_air_date        DATE,
    vote_average         FLOAT,
    vote_count           INT,
    original_language    VARCHAR(10),
    adult                BOOLEAN      NOT NULL DEFAULT false,
    runtime              INT,
    episode_run_time     INT,
    status               VARCHAR(100),
    imdb_id              VARCHAR(20),
    certification_ru     VARCHAR(10),
    certification_us     VARCHAR(10),
    kinopoisk_id         BIGINT,
    myshows_id           INT,
    myshows_show_id      INT,
    myshows_status       VARCHAR(100),
    myshows_total_episodes INT,
    myshows_network      VARCHAR(200),
    myshows_next_air_date VARCHAR(20),
    myshows_updated_at   TIMESTAMPTZ,
    genres               JSONB,
    production_countries JSONB,
    keywords             JSONB,
    number_of_seasons    INT,
    number_of_episodes   INT,
    seasons              JSONB,
    last_ep_season       INT,
    last_ep_number       INT,
    next_ep_air_date     VARCHAR(20),
    episodes_synced_at   TIMESTAMPTZ,
    age_rating           INT,
    year                 INT,
    rutor_category       VARCHAR(50),
    best_video_quality   INT          NOT NULL DEFAULT 0,
    latest_torrent_date  TIMESTAMPTZ,
    tmdb_updated_at      TIMESTAMPTZ,
    updated_at           TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT uq_media_card_tmdb UNIQUE (tmdb_id, media_type)
);

CREATE INDEX IF NOT EXISTS idx_media_cards_tmdb_id  ON media_cards (tmdb_id);
CREATE INDEX IF NOT EXISTS idx_media_cards_category ON media_cards (rutor_category);
CREATE INDEX IF NOT EXISTS idx_media_cards_language ON media_cards (original_language);

-- ─── Episodes ─────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS episodes (
    tmdb_show_id  INT         NOT NULL,
    season        SMALLINT    NOT NULL,
    episode       SMALLINT    NOT NULL,
    title         VARCHAR(500),
    duration_sec  INT,
    is_special    BOOLEAN     NOT NULL DEFAULT false,
    myshows_ep_id INT,
    hash          VARCHAR(20),
    air_date      DATE,
    PRIMARY KEY (tmdb_show_id, season, episode)
);

CREATE INDEX IF NOT EXISTS idx_episodes_tmdb_show_id ON episodes (tmdb_show_id);

-- ─── MyShows global mapping ───────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS myshows_items (
    id         BIGSERIAL   PRIMARY KEY,
    myshows_id INT         UNIQUE NOT NULL,
    tmdb_id    INT         NOT NULL,
    media_type VARCHAR(10) NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_myshows_items_tmdb_id ON myshows_items (tmdb_id);

CREATE TABLE IF NOT EXISTS myshows_watching (
    device_id        BIGINT       NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    profile_id VARCHAR(100) NOT NULL DEFAULT '',
    item_id          BIGINT       NOT NULL REFERENCES myshows_items(id) ON DELETE CASCADE,
    unwatched_count  INT,
    next_episode     VARCHAR(20),
    progress_marker  VARCHAR(100),
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    PRIMARY KEY (device_id, profile_id, item_id)
);

CREATE TABLE IF NOT EXISTS myshows_user_status (
    device_id        BIGINT       NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    profile_id VARCHAR(100) NOT NULL DEFAULT '',
    item_id          BIGINT       NOT NULL REFERENCES myshows_items(id) ON DELETE CASCADE,
    cache_type       VARCHAR(20)  NOT NULL,
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    PRIMARY KEY (device_id, profile_id, item_id)
);

-- ─── Torrents ─────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS torrents (
    hash       TEXT         PRIMARY KEY,
    tmdb_id    BIGINT,
    media_type TEXT,
    card_id    VARCHAR(30)
);

CREATE INDEX IF NOT EXISTS idx_torrents_card_id ON torrents (card_id) WHERE card_id IS NOT NULL;

-- ─── Statistics ───────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS stats_myshows_users (
    id       BIGSERIAL    PRIMARY KEY,
    login    VARCHAR(100) NOT NULL,
    date     VARCHAR(10)  NOT NULL,
    requests INT          NOT NULL DEFAULT 1,
    CONSTRAINT uq_myshows_login_date UNIQUE (login, date)
);

CREATE TABLE IF NOT EXISTS stats_api_users (
    id       BIGSERIAL   PRIMARY KEY,
    ip       VARCHAR(50) NOT NULL,
    date     VARCHAR(10) NOT NULL,
    requests INT         NOT NULL DEFAULT 1,
    country  VARCHAR(100),
    city     VARCHAR(100),
    region   VARCHAR(100),
    flag_emoji VARCHAR(10),
    CONSTRAINT uq_api_ip_date UNIQUE (ip, date)
);

CREATE TABLE IF NOT EXISTS stats_category_requests (
    id       BIGSERIAL    PRIMARY KEY,
    category VARCHAR(200) NOT NULL,
    ip       VARCHAR(50)  NOT NULL,
    date     VARCHAR(10)  NOT NULL,
    requests INT          NOT NULL DEFAULT 1,
    CONSTRAINT uq_category_ip_date UNIQUE (category, ip, date)
);

-- ─── Play events (popularity tracking) ───────────────────────────────────────
CREATE TABLE IF NOT EXISTS media_play_events (
    card_id VARCHAR(100) NOT NULL REFERENCES media_cards(card_id) ON DELETE CASCADE,
    ident   VARCHAR(100) NOT NULL,
    date    DATE         NOT NULL DEFAULT CURRENT_DATE,
    PRIMARY KEY (card_id, ident, date)
);

CREATE INDEX IF NOT EXISTS idx_play_events_date ON media_play_events (date);

-- ─── Migrations: add columns to existing tables ───────────────────────────────
-- These are safe to run on any existing DB (IF NOT EXISTS is idempotent).
ALTER TABLE media_cards ADD COLUMN IF NOT EXISTS certification_us       VARCHAR(10);
ALTER TABLE media_cards ADD COLUMN IF NOT EXISTS myshows_show_id        INT;
ALTER TABLE media_cards ADD COLUMN IF NOT EXISTS myshows_status         VARCHAR(100);
ALTER TABLE media_cards ADD COLUMN IF NOT EXISTS myshows_total_episodes INT;
ALTER TABLE media_cards ADD COLUMN IF NOT EXISTS myshows_network        VARCHAR(200);
ALTER TABLE media_cards ADD COLUMN IF NOT EXISTS myshows_next_air_date  VARCHAR(20);
ALTER TABLE media_cards ADD COLUMN IF NOT EXISTS myshows_updated_at     TIMESTAMPTZ;
ALTER TABLE media_cards ADD COLUMN IF NOT EXISTS production_countries   JSONB;
ALTER TABLE media_cards ADD COLUMN IF NOT EXISTS keywords               JSONB;
ALTER TABLE media_cards ADD COLUMN IF NOT EXISTS year                   INT;
ALTER TABLE media_cards ADD COLUMN IF NOT EXISTS created_at             TIMESTAMPTZ NOT NULL DEFAULT now();
ALTER TABLE timecodes   ADD COLUMN IF NOT EXISTS created_at             TIMESTAMPTZ NOT NULL DEFAULT now();
CREATE UNIQUE INDEX IF NOT EXISTS uq_devices_user_name ON devices (user_id, lower(name));
