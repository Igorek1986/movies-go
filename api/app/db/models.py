from sqlalchemy import (
    Column,
    Index,
    Integer,
    BigInteger,
    SmallInteger,
    String,
    Boolean,
    DateTime,
    Date,
    Float,
    ForeignKey,
    ForeignKeyConstraint,
    UniqueConstraint,
    Text,
    JSON,
)
from sqlalchemy.sql import func
from app.db.database import Base


# Роли пользователей
USER_ROLES = ("simple", "premium", "super")


class AppSetting(Base):
    """Настройки приложения — ключ/значение. Изменяются через админку без перезапуска."""

    __tablename__ = "app_settings"

    key        = Column(String(100), primary_key=True)
    value      = Column(Text, nullable=False)
    updated_at = Column(DateTime(timezone=True), server_default=func.now(), onupdate=func.now())

    def __repr__(self):
        return f"<AppSetting({self.key}={self.value})>"


class User(Base):
    """Модель пользователя — только для веб-авторизации."""

    __tablename__ = "users"

    id = Column(Integer, primary_key=True, index=True)
    username = Column(String(50), unique=True, nullable=False, index=True)
    password_hash = Column(String(255), nullable=False)
    # session_key используется только для cookie-авторизации в веб-интерфейсе.
    # Для доступа к API (Lampa) используется Device.token.
    session_key = Column(String(64), unique=True, nullable=True, index=True)
    # Роль: "simple" (3 уст.), "premium" (8 уст.), "super" (без лимита)
    role = Column(String(20), nullable=False, default="simple", server_default="simple")
    # Флаг администратора сайта: доступ к /admin и /stats без пароля
    is_admin = Column(Boolean, nullable=False, default=False, server_default="false")
    # TOTP 2FA
    totp_secret  = Column(String(64), nullable=True)
    totp_enabled = Column(Boolean, nullable=False, default=False, server_default="false")
    backup_codes = Column(Text, nullable=True)   # JSON list of SHA-256 hex digests
    # Premium subscription expiry
    premium_until       = Column(DateTime(timezone=True), nullable=True)
    # Grace period: timecodes kept N days after premium expiry if over simple limit
    timecode_grace_until = Column(DateTime(timezone=True), nullable=True)
    # Deferred Telegram notification: deliver at this UTC time
    notify_premium_after = Column(DateTime(timezone=True), nullable=True)
    # Type of deferred notification: "warning" (3 days before expiry) or "expired"
    notify_type = Column(String(20), nullable=True)
    # Whether the 3-day advance warning has been sent (reset when premium is re-granted)
    premium_warned = Column(Boolean, nullable=False, default=False, server_default="false")
    # User's preferred timezone (e.g. "Europe/Moscow"); None = use server default
    timezone = Column(String(50), nullable=True)
    # Notification delivery window (local hours, inclusive start, exclusive end)
    notify_start = Column(Integer, nullable=False, default=9,  server_default="9")
    notify_end   = Column(Integer, nullable=False, default=22, server_default="22")
    # Master switch: False = no Telegram notifications at all (including login alerts)
    notifications_enabled = Column(Boolean, nullable=False, default=True, server_default="true")
    # Inactive user auto-deletion
    last_active_at        = Column(Date, nullable=True)
    inactive_warned       = Column(Boolean, nullable=False, default=False, server_default="false")
    notify_inactive_after = Column(DateTime(timezone=True), nullable=True)
    # Account block (admin action)
    blocked_at   = Column(DateTime(timezone=True), nullable=True)
    block_reason = Column(String(500), nullable=True)
    created_at = Column(DateTime(timezone=True), server_default=func.now())
    updated_at = Column(DateTime(timezone=True), onupdate=func.now())

    def __repr__(self):
        return f"<User(id={self.id}, username={self.username}, role={self.role})>"


class Device(Base):
    """Устройство пользователя. Каждое устройство имеет уникальный токен для Lampa."""

    __tablename__ = "devices"

    id = Column(Integer, primary_key=True, index=True)
    user_id = Column(Integer, ForeignKey("users.id", ondelete="CASCADE"), nullable=False, index=True)
    name = Column(String(100), nullable=False, default="Основное")
    # Хранится plaintext — нужен для device activation flow.
    token = Column(String(64), unique=True, nullable=False, index=True)
    created_at = Column(DateTime(timezone=True), server_default=func.now())

    def __repr__(self):
        return f"<Device(id={self.id}, user_id={self.user_id}, name={self.name})>"


class DeviceCode(Base):
    """Одноразовый код для привязки устройства (Lampa) к Device без ручного ввода токена."""

    __tablename__ = "device_codes"

    id = Column(Integer, primary_key=True, index=True)
    code = Column(String(6), unique=True, nullable=False, index=True)  # формат: "483921"
    user_id = Column(Integer, ForeignKey("users.id", ondelete="CASCADE"), nullable=True)
    device_id = Column(Integer, ForeignKey("devices.id", ondelete="CASCADE"), nullable=True)
    expires_at = Column(DateTime(timezone=True), nullable=False)
    created_at = Column(DateTime(timezone=True), server_default=func.now())

    def __repr__(self):
        return f"<DeviceCode(code={self.code}, linked={self.device_id is not None})>"


class Timecode(Base):
    """Прогресс просмотра — привязан к устройству и опциональному профилю Lampa."""

    __tablename__ = "timecodes"

    id = Column(Integer, primary_key=True, index=True)
    device_id = Column(
        Integer, ForeignKey("devices.id", ondelete="CASCADE"), nullable=False, index=True
    )
    # Опциональный ID профиля из встроенной системы профилей Lampa.
    # Пустая строка означает «без профиля» (дефолт).
    lampa_profile_id = Column(String(100), nullable=False, default="", server_default="")
    card_id = Column(String(100), nullable=False, index=True)  # "{tmdb_id}_movie" или "_tv"
    item = Column(String(100), nullable=False, index=True)     # хэш эпизода/фильма (lampa_hash)
    data = Column(Text, nullable=False)                        # JSON: {duration, time, percent}
    counted_at = Column(Date, nullable=True)   # дата последнего засчитанного просмотра (лимит 1/день)
    view_count = Column(Integer, nullable=False, default=0, server_default="0")  # кол-во просмотров этого item
    updated_at = Column(
        DateTime(timezone=True), server_default=func.now(), onupdate=func.now()
    )

    __table_args__ = (
        UniqueConstraint(
            "device_id", "lampa_profile_id", "card_id", "item",
            name="uq_timecode_unique"
        ),
    )

    def __repr__(self):
        return f"<Timecode(device_id={self.device_id}, card_id={self.card_id}, item={self.item})>"


class MediaCard(Base):
    """TMDB-метаданные карточек. Пишется Python (upsert_tmdb_cache) и Go-парсером.

    Два пути записи:
      - Python: conflict target = card_id (PK)
      - Go:     conflict target = (tmdb_id, media_type) (уникальный индекс)
    """

    __tablename__ = "media_cards"
    __table_args__ = (
        # Нужен Go-парсеру: ON CONFLICT (tmdb_id, media_type)
        UniqueConstraint("tmdb_id", "media_type", name="uq_media_cards_tmdb"),
        # Ускоряет фильтрацию по категориям (media_type + language)
        Index("idx_media_cards_type_lang", "media_type", "original_language"),
        # Ускоряет сортировку по дате выхода
        Index("idx_media_cards_release_date", "release_date"),
        # Ускоряет сортировку категорий по дате добавления торрента
        Index("idx_media_cards_latest_torrent", "latest_torrent_date"),
    )

    # ── Ключи ────────────────────────────────────────────────────────────────
    card_id    = Column(String(100), primary_key=True)  # "{tmdb_id}_movie" | "{tmdb_id}_tv"
    tmdb_id    = Column(BigInteger, nullable=False, index=True)
    media_type = Column(String(5), nullable=False)       # "movie" | "tv"

    # ── TMDB базовые ─────────────────────────────────────────────────────────
    title          = Column(Text, nullable=True)
    original_title = Column(Text, nullable=True)
    overview       = Column(Text, nullable=True)
    poster_path    = Column(Text, nullable=True)
    backdrop_path  = Column(Text, nullable=True)
    vote_average   = Column(Float, nullable=True)
    vote_count     = Column(Integer, nullable=True)
    runtime        = Column(Integer, nullable=True)       # минуты (фильм) или средняя серия (тв)
    status         = Column(Text, nullable=True)          # "Released", "Returning Series", etc.
    imdb_id        = Column(String(20), nullable=True)    # tt1234567
    original_language = Column(String(10), nullable=True)
    adult          = Column(Boolean, nullable=False, server_default="false")

    # ── TMDB даты ────────────────────────────────────────────────────────────
    # release_date хранит: дату выхода фильма ИЛИ first_air_date сериала (для Python-compat)
    release_date   = Column(String(20), nullable=True)
    first_air_date = Column(String(20), nullable=True)   # tv: Go пишет сюда, Python читает
    last_air_date  = Column(String(20), nullable=True)
    next_ep_air_date = Column(String(20), nullable=True) # "" = нет; NULL = не обновлено

    # ── TMDB расширенные ─────────────────────────────────────────────────────
    genres              = Column(JSON, nullable=True)     # [{"id":28,"name":"Action"}]
    age_rating          = Column(SmallInteger, nullable=True)  # 0/6/12/16/18
    certification_ru    = Column(String(10), nullable=True)
    certification_us    = Column(String(10), nullable=True)
    production_countries = Column(JSON, nullable=True)    # [{"iso_3166_1":"US","name":"..."}]
    keywords            = Column(JSON, nullable=True)     # [{"id":1,"name":"..."}]

    # ── TV только ────────────────────────────────────────────────────────────
    number_of_seasons  = Column(Integer, nullable=True)
    number_of_episodes = Column(Integer, nullable=True)
    # seasons: Go пишет как JSON; Python пишет seasons_json как TEXT (legacy)
    seasons            = Column(JSON, nullable=True)      # Go: список сезонов TMDB
    seasons_json       = Column(Text, nullable=True)      # Python legacy (episodes sync)
    last_ep_season     = Column(Integer, nullable=True)   # last_episode_to_air.season_number
    last_ep_number     = Column(Integer, nullable=True)   # last_episode_to_air.episode_number
    episode_run_time   = Column(Integer, nullable=True)   # средняя продолжительность серии, мин

    # ── MyShows (глобальные данные, не per-user) ─────────────────────────────
    myshows_id         = Column(Integer, nullable=True)   # ID в MyShows (Go пишет)
    myshows_show_id    = Column(Integer, nullable=True)   # alias, Python legacy
    myshows_status     = Column(Text, nullable=True)      # "Ended" / "Returning" / etc.
    myshows_total_episodes = Column(Integer, nullable=True)
    myshows_network    = Column(Text, nullable=True)
    myshows_next_air_date = Column(String(20), nullable=True)

    # ── Кинопоиск ────────────────────────────────────────────────────────────
    kinopoisk_id = Column(BigInteger, nullable=True)

    # ── Вспомогательные ──────────────────────────────────────────────────────
    latest_torrent_date = Column(DateTime(timezone=True), nullable=True)
    best_video_quality  = Column(SmallInteger, nullable=True)
    rutor_category      = Column(String(50), nullable=True)
    year               = Column(String(4), nullable=True)  # Python convenience
    episodes_synced_at = Column(DateTime(timezone=True), nullable=True)
    tmdb_updated_at    = Column(DateTime(timezone=True), nullable=True)
    myshows_updated_at = Column(DateTime(timezone=True), nullable=True)
    updated_at         = Column(DateTime(timezone=True), server_default=func.now(), onupdate=func.now())

    def __repr__(self):
        return f"<MediaCard(card_id={self.card_id}, title={self.title})>"


class Torrent(Base):
    """Торрент с rutor.info. Пишется только Go-парсером."""

    __tablename__ = "torrents"
    __table_args__ = (
        ForeignKeyConstraint(
            ["tmdb_id", "media_type"],
            ["media_cards.tmdb_id", "media_cards.media_type"],
            ondelete="SET NULL",
            name="fk_torrents_media_card",
        ),
        Index("idx_torrents_tmdb", "tmdb_id", "media_type"),
        Index("idx_torrents_category", "rutor_category"),
        Index("idx_torrents_video_quality", "video_quality"),
        Index("idx_torrents_create_date", "create_date"),
        Index("idx_torrents_tmdb_searched", "tmdb_searched_at",
              postgresql_where=("tmdb_id IS NULL")),
    )

    hash           = Column(String(40), primary_key=True)
    tmdb_id        = Column(BigInteger, nullable=True)
    media_type     = Column(String(5), nullable=True)      # "movie" | "tv"
    video_quality  = Column(SmallInteger, nullable=True)   # 0=SD 100+=720p 200+=1080p 300+=4K
    audio_quality  = Column(SmallInteger, nullable=True)
    rutor_category = Column(String(20), nullable=True)     # movies/series/cartoon/anime/...
    create_date    = Column(DateTime(timezone=True), nullable=True)
    size           = Column(Text, nullable=True)
    seed           = Column(Integer, nullable=True)
    peer           = Column(Integer, nullable=True)
    magnet         = Column(Text, nullable=True)
    link           = Column(Text, nullable=True)
    title          = Column(Text, nullable=True)            # оригинальное название с rutor
    tmdb_searched_at = Column(DateTime(timezone=True), nullable=True)  # когда искали в TMDB (не нашли)

    def __repr__(self):
        return f"<Torrent(hash={self.hash[:8]}, title={self.title})>"


class Episode(Base):
    """Метаданные эпизода сериала. Источники: MyShows (runtime, isSpecial), Lampa (duration_sec)."""

    __tablename__ = "episodes"

    tmdb_show_id  = Column(Integer, primary_key=True)
    season        = Column(SmallInteger, primary_key=True)
    episode       = Column(SmallInteger, primary_key=True)
    title         = Column(String(500), nullable=True)    # название из MyShows
    duration_sec  = Column(Integer, nullable=True)        # уточняется из Lampa/Lampac
    is_special    = Column(Boolean, default=False, nullable=False, server_default="false")
    myshows_ep_id = Column(Integer, nullable=True)
    hash          = Column(String(20), nullable=True)     # lampa_hash для быстрого поиска
    air_date      = Column(Date, nullable=True)           # дата выхода из MyShows

    def __repr__(self):
        return f"<Episode(show={self.tmdb_show_id}, s{self.season:02d}e{self.episode:02d}, special={self.is_special})>"


class MyShowsItem(Base):
    """Глобальный маппинг myshows_id → tmdb_id/media_type. Общий для всех пользователей."""

    __tablename__ = "myshows_items"

    id         = Column(Integer, primary_key=True, index=True)
    myshows_id = Column(Integer, unique=True, nullable=False, index=True)
    tmdb_id    = Column(Integer, nullable=False, index=True)
    media_type = Column(String(10), nullable=False)  # tv / movie


class MyShowsWatching(Base):
    """Сериалы с непросмотренными эпизодами. Пишет только fetchFromMyShowsAPI (POST /watching)."""

    __tablename__ = "myshows_watching"

    device_id        = Column(Integer, ForeignKey("devices.id", ondelete="CASCADE"), primary_key=True)
    lampa_profile_id = Column(String(100), primary_key=True, server_default="")
    item_id          = Column(Integer, ForeignKey("myshows_items.id", ondelete="CASCADE"), primary_key=True)
    unwatched_count  = Column(Integer, nullable=True)
    next_episode     = Column(String(20), nullable=True)    # "S01E03"
    progress_marker  = Column(String(100), nullable=True)   # "3 из 12"
    updated_at       = Column(DateTime(timezone=True), server_default=func.now(), onupdate=func.now())


class MyShowsUserStatus(Base):
    """Статус сериала/фильма для конкретного пользователя и профиля Lampa."""

    __tablename__ = "myshows_user_status"

    device_id        = Column(Integer, ForeignKey("devices.id", ondelete="CASCADE"), primary_key=True)
    lampa_profile_id = Column(String(100), primary_key=True, server_default="")
    item_id          = Column(Integer, ForeignKey("myshows_items.id", ondelete="CASCADE"), primary_key=True)
    cache_type       = Column(String(20), nullable=False)   # watchlist / watched / cancelled
    updated_at       = Column(DateTime(timezone=True), server_default=func.now(), onupdate=func.now())


class LampaProfile(Base):
    """Человеческое название для lampa_profile_id внутри устройства."""

    __tablename__ = "lampa_profiles"

    id               = Column(Integer, primary_key=True, index=True)
    device_id        = Column(Integer, ForeignKey("devices.id", ondelete="CASCADE"), nullable=False, index=True)
    lampa_profile_id = Column(String(100), nullable=False)
    name             = Column(String(100), nullable=False, default="")
    icon             = Column(String(20), nullable=True)   # e.g. "id1", "id3"
    favorite         = Column(Text, nullable=True)         # JSON: Lampa favorite object
    child            = Column(Boolean, nullable=False, server_default="false")
    params           = Column(JSON, nullable=False, server_default="{}")

    __table_args__ = (
        UniqueConstraint("device_id", "lampa_profile_id", name="uq_lampa_profile"),
    )

    def __repr__(self):
        return f"<LampaProfile(device_id={self.device_id}, profile_id={self.lampa_profile_id}, name={self.name})>"


class MyShowsUser(Base):
    """Статистика обращений пользователей MyShows."""

    __tablename__ = "stats_myshows_users"

    id = Column(Integer, primary_key=True, index=True)
    login = Column(String(100), nullable=False, index=True)
    date = Column(String(10), nullable=False, index=True)   # YYYY-MM-DD
    requests = Column(Integer, default=1, nullable=False)

    __table_args__ = (
        UniqueConstraint("login", "date", name="uq_myshows_login_date"),
    )


class ApiUser(Base):
    """Статистика обращений по IP (обычные пользователи API)."""

    __tablename__ = "stats_api_users"

    id = Column(Integer, primary_key=True, index=True)
    ip = Column(String(50), nullable=False, index=True)
    date = Column(String(10), nullable=False, index=True)   # YYYY-MM-DD
    requests = Column(Integer, default=1, nullable=False)
    country = Column(String(100), nullable=True)
    city = Column(String(100), nullable=True)
    region = Column(String(100), nullable=True)
    flag_emoji = Column(String(10), nullable=True)

    __table_args__ = (
        UniqueConstraint("ip", "date", name="uq_api_ip_date"),
    )


class CategoryRequest(Base):
    """Статистика обращений к категориям контента."""

    __tablename__ = "stats_category_requests"

    id = Column(Integer, primary_key=True, index=True)
    category = Column(String(200), nullable=False, index=True)
    ip = Column(String(50), nullable=False, index=True)
    date = Column(String(10), nullable=False, index=True)   # YYYY-MM-DD
    requests = Column(Integer, default=1, nullable=False)

    __table_args__ = (
        UniqueConstraint("category", "ip", "date", name="uq_category_ip_date"),
    )


class PasswordResetToken(Base):
    """Одноразовый токен для сброса пароля через Telegram."""

    __tablename__ = "password_reset_tokens"

    id = Column(Integer, primary_key=True, index=True)
    user_id = Column(Integer, ForeignKey("users.id", ondelete="CASCADE"), nullable=False)
    token = Column(String(64), unique=True, nullable=False, index=True)
    expires_at = Column(DateTime(timezone=True), nullable=False)
    created_at = Column(DateTime(timezone=True), server_default=func.now())

    def __repr__(self):
        return f"<PasswordResetToken(user_id={self.user_id})>"


class TelegramUser(Base):
    """Привязка Telegram-аккаунта к пользователю сайта."""

    __tablename__ = "telegram_users"

    id          = Column(Integer, primary_key=True, index=True)
    user_id     = Column(Integer, ForeignKey("users.id", ondelete="CASCADE"), unique=True, nullable=False)
    telegram_id = Column(BigInteger, unique=True, nullable=False, index=True)
    username    = Column(String(100), nullable=True)   # @handle без @
    linked_at   = Column(DateTime(timezone=True), server_default=func.now())

    def __repr__(self):
        return f"<TelegramUser(user_id={self.user_id}, telegram_id={self.telegram_id})>"


class TelegramLinkCode(Base):
    """Одноразовый код для привязки Telegram-аккаунта (TTL 10 мин)."""

    __tablename__ = "telegram_link_codes"

    id         = Column(Integer, primary_key=True, index=True)
    user_id    = Column(Integer, ForeignKey("users.id", ondelete="CASCADE"), nullable=False)
    code       = Column(String(6), unique=True, nullable=False, index=True)
    expires_at = Column(DateTime(timezone=True), nullable=False)
    created_at = Column(DateTime(timezone=True), server_default=func.now())

    def __repr__(self):
        return f"<TelegramLinkCode(user_id={self.user_id}, code={self.code})>"


class Session(Base):
    """Веб-сессия пользователя (cookie session_key → Session.key)."""

    __tablename__ = "sessions"

    id         = Column(Integer, primary_key=True, index=True)
    user_id    = Column(Integer, ForeignKey("users.id", ondelete="CASCADE"), nullable=False, index=True)
    key        = Column(String(64), unique=True, nullable=False, index=True)
    expires_at = Column(DateTime(timezone=True), nullable=False)
    created_at = Column(DateTime(timezone=True), server_default=func.now())
    ip         = Column(String(50), nullable=True)
    user_agent = Column(String(500), nullable=True)

    def __repr__(self):
        return f"<Session(user_id={self.user_id}, ip={self.ip})>"


class TrustedDevice(Base):
    """Доверенное устройство пользователя (долгоживущий cookie device_token)."""

    __tablename__ = "trusted_devices"

    id           = Column(Integer, primary_key=True, index=True)
    user_id      = Column(Integer, ForeignKey("users.id", ondelete="CASCADE"), nullable=False, index=True)
    token        = Column(String(64), unique=True, nullable=False, index=True)
    created_at   = Column(DateTime(timezone=True), server_default=func.now())
    last_used_at = Column(DateTime(timezone=True), nullable=True)

    def __repr__(self):
        return f"<TrustedDevice(user_id={self.user_id})>"


class Totp2faPending(Base):
    """Временный токен ожидающего 2FA-подтверждения входа (TTL 10 мин)."""

    __tablename__ = "totp_2fa_pending"

    id         = Column(Integer, primary_key=True, index=True)
    user_id    = Column(Integer, ForeignKey("users.id", ondelete="CASCADE"), nullable=False)
    token      = Column(String(64), unique=True, nullable=False, index=True)
    expires_at = Column(DateTime(timezone=True), nullable=False)
    created_at = Column(DateTime(timezone=True), server_default=func.now())

    def __repr__(self):
        return f"<Totp2faPending(user_id={self.user_id})>"


class PluginSettings(Base):
    """Настройки плагинов Lampa — синхронизируются между устройствами одного пользователя.

    Изоляция по профилю: каждая комбинация (user_id, lampa_profile_id, plugin) — отдельная строка.
    lampa_profile_id = '' означает «без профиля».
    """

    __tablename__ = "plugin_settings"

    user_id          = Column(Integer, ForeignKey("users.id", ondelete="CASCADE"), nullable=False, primary_key=True)
    lampa_profile_id = Column(String(100), nullable=False, default="", server_default="''", primary_key=True)
    plugin           = Column(String(100), nullable=False, primary_key=True)
    settings         = Column(Text, nullable=False, default="{}", server_default="'{}'")
    updated_at       = Column(DateTime(timezone=True), server_default=func.now(), onupdate=func.now())

    def __repr__(self):
        return f"<PluginSettings(user_id={self.user_id}, lampa_profile_id={self.lampa_profile_id!r}, plugin={self.plugin})>"




class SupportMessage(Base):
    """Сообщение в чате поддержки между пользователем и администратором."""

    __tablename__ = "support_messages"

    id               = Column(Integer, primary_key=True, index=True)
    # Telegram пользователя (не обязательно привязанного к аккаунту сайта)
    user_telegram_id = Column(BigInteger, nullable=False, index=True)
    user_username    = Column(String(100), nullable=True)
    # direction: 'in' = user→admin, 'out' = admin→user
    direction        = Column(String(3), nullable=False)
    text             = Column(Text, nullable=False)
    # ID уведомления в чате конкретного администратора (для маршрутизации ответов)
    admin_telegram_id = Column(BigInteger, nullable=True, index=True)
    admin_msg_id      = Column(Integer, nullable=True)
    is_read           = Column(Boolean, nullable=False, default=False, server_default="false")
    created_at        = Column(DateTime(timezone=True), server_default=func.now())

    def __repr__(self):
        return f"<SupportMessage(id={self.id}, direction={self.direction}, from={self.user_telegram_id})>"
