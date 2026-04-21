import asyncio
import json
import logging
import re
import httpx
from concurrent.futures import ThreadPoolExecutor
from contextlib import asynccontextmanager
from datetime import datetime, date as _date
from math import ceil
from pathlib import Path
from typing import Any, Dict, Tuple
from logging import DEBUG, INFO

import requests
from fastapi import FastAPI, Header, Query, status, Request, HTTPException, Depends
from fastapi.middleware.cors import CORSMiddleware
from fastapi.middleware.gzip import GZipMiddleware
from fastapi.responses import JSONResponse, PlainTextResponse, HTMLResponse, Response, RedirectResponse
from app.templates import get_templates
from fastapi.staticfiles import StaticFiles
from fastapi.responses import FileResponse
from sqlalchemy import select, func, case, Float
from sqlalchemy.dialects.postgresql import insert as pg_insert
from sqlalchemy.ext.asyncio import AsyncSession

from app.db.database import init_db, async_session_maker
from app.db.models import MediaCard, User, Timecode, Episode
from app.api.dependencies import get_current_user
from app.config import get_settings
from app.api import auth, myshows_sync, timecodes as timecodes_router
from app.api import devices
from app.api import sessions as sessions_router
from app.api import telegram as telegram_router
from app.api import tg_miniapp as tg_miniapp_router
from app.api import episodes as episodes_router
from app.api import plugin_settings as plugin_settings_router
from app.admin import router as admin_router
from app.api.dependencies import get_device_by_token
from app.api.timecodes import load_device_timecodes, get_watched_movie_ids, get_watched_tv_ids
from app.utils import lampa_hash, build_episode_hash_string
from app import settings_cache as _sc
from app.db.database import get_db

settings = get_settings()

from app import myshows
from app import stats

TMDB_TOKEN = settings.TMDB_TOKEN
BANNED_PATTERNS = settings.banned_patterns_list

# Получаем путь к директории, где находится текущий скрипт
BASE_DIR = Path(__file__).parent.parent
BLOCKED_JSON_PATH = BASE_DIR / "blocked.json"
tmdb_cache: Dict[Tuple[str, int], Any] = None
with open(BLOCKED_JSON_PATH, "r", encoding="utf-8") as f:
    BLOCKED_RESPONSE = json.load(f)

STATIC_DIR = BASE_DIR / "static"
PLUGINS_DIR = BASE_DIR / "lampa-plugins"
PLUGINS_DIR.mkdir(exist_ok=True)
# Настройка логирования
logging.basicConfig(
    level=DEBUG if settings.DEBUG else INFO,
    format="%(asctime)s - %(levelname)s - %(message)s",
    handlers=[logging.StreamHandler()],
)

logger = logging.getLogger(__name__)

# Отключаем verbose DEBUG-логи httpx/httpcore
logging.getLogger("httpx").setLevel(logging.WARNING)
logging.getLogger("httpcore").setLevel(logging.WARNING)


@asynccontextmanager
async def lifespan(app: FastAPI):
    """Собственный обработчик жизненного цикла приложения"""
    global tmdb_cache

    stats.init_stats()

    # === Startup ===
    print("🔍 Connecting to:", settings.DATABASE_URL)
    await init_db()
    print("✅ Database tables created")

    # Загрузка настроек приложения из БД
    from app import settings_cache
    async with async_session_maker() as _settings_db:
        await settings_cache.load(_settings_db)

    # Загрузка TMDB-кэша из PostgreSQL
    tmdb_cache = await load_cache_from_db()
    logger.info(f"TMDB кэш загружен из БД, записей: {len(tmdb_cache)}")
    logger.info(f"Рабочая директория: {BASE_DIR}")

    # Инициализация Telegram-бота
    _polling_task = None
    if settings.TELEGRAM_BOT_TOKEN:
        from app.bot import init_bot

        bot, dp = init_bot(settings.TELEGRAM_BOT_TOKEN)
        if settings.TELEGRAM_USE_POLLING:
            try:
                await bot.delete_webhook(drop_pending_updates=True)
            except Exception as e:
                logger.warning(f"Telegram недоступен при старте (delete_webhook): {e}")
            _polling_task = asyncio.create_task(
                dp.start_polling(bot, handle_signals=False)
            )
            logger.info("Telegram bot started in polling mode")
        else:
            # webhook регистрируется через dp.startup hook в bot.py
            try:
                await dp.emit_startup(bot=bot)
            except Exception as e:
                logger.warning(f"Telegram недоступен при старте (webhook): {e}")
    else:
        logger.warning("TELEGRAM_BOT_TOKEN не задан — бот отключён")

    # Запуск фоновых задач (premium expiry check, etc.)
    from app.tasks import start_tasks
    start_tasks()

    yield  # Приложение работает

    # Shutdown
    from app.tasks import stop_tasks
    stop_tasks()
    if settings.TELEGRAM_BOT_TOKEN:
        from app.bot import get_bot, get_dp

        b = get_bot()
        d = get_dp()
        if _polling_task:
            _polling_task.cancel()
        elif d and b:
            await d.emit_shutdown(bot=b)
        if b:
            await b.session.close()


# app = FastAPI()
app = FastAPI(
    lifespan=lifespan,
    docs_url="/docs" if settings.DEBUG else None,
    redoc_url="/redoc" if settings.DEBUG else None,
    openapi_url="/openapi.json" if settings.DEBUG else None,
)
app.add_middleware(GZipMiddleware, minimum_size=1000)
app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)

app.mount("/static", StaticFiles(directory=STATIC_DIR), name="static")
app.mount("/img/profiles", StaticFiles(directory=STATIC_DIR / "profileIcons"), name="profile_icons")


@app.get("/favicon.ico")
async def favicon():
    return FileResponse("static/favicon/favicon.ico", media_type="image/x-icon")



app.include_router(auth.router)
app.include_router(episodes_router.router)
app.include_router(devices.router)
app.include_router(sessions_router.router)
app.include_router(timecodes_router.router)
app.include_router(myshows.router)
app.include_router(stats.router)
app.include_router(myshows_sync.router)
app.include_router(admin_router)
app.include_router(telegram_router.router)
app.include_router(tg_miniapp_router.router)
app.include_router(plugin_settings_router.router)


@app.middleware("http")
async def block_banned_origins(request: Request, call_next):
    origin = request.headers.get("origin")

    if is_banned_origin(origin):
        logger.warning(f"Blocked request from origin: {origin}")

        return JSONResponse(
            status_code=200,
            content=BLOCKED_RESPONSE,
            headers={
                "Cache-Control": "no-store, no-cache, must-revalidate, proxy-revalidate",
                "Pragma": "no-cache",
                "Expires": "0",
                "Access-Control-Allow-Origin": origin or "*",
                "Access-Control-Allow-Credentials": "true",
            },
        )

    return await call_next(request)


@app.middleware("http")
async def serve_lampa_plugins(request: Request, call_next):
    if request.method == "GET":
        rel = request.url.path.lstrip("/")
        if rel:
            try:
                plugin_path = (PLUGINS_DIR / rel).resolve()
                plugin_path.relative_to(PLUGINS_DIR.resolve())
                if plugin_path.is_file():
                    response = FileResponse(str(plugin_path))
                    response.headers["Access-Control-Allow-Origin"] = "*"
                    return response
            except (ValueError, OSError):
                pass
    return await call_next(request)


def is_banned_origin(origin: str | None) -> bool:
    if not origin or origin == "null":
        return False

    origin = origin.lower()
    return any(pattern.lower() in origin for pattern in BANNED_PATTERNS)


logger.debug("Настройки окружения загружены успешно")

executor = ThreadPoolExecutor(max_workers=10)  # Пул потоков для запросов


def _extract_tmdb_fields(media_type: str, data: dict) -> dict:
    """Извлекает только нужные поля из полного ответа TMDB API для in-memory кэша."""
    base = {
        "poster_path": data.get("poster_path", ""),
        "backdrop_path": data.get("backdrop_path", ""),
        "overview": data.get("overview", ""),
        "vote_average": data.get("vote_average", 0),
    }
    if media_type == "movie":
        base.update(
            {
                "title": data.get("title", ""),
                "original_title": data.get("original_title", ""),
                "release_date": data.get("release_date", ""),
            }
        )
    else:  # tv
        base.update(
            {
                "name": data.get("name", ""),
                "original_name": data.get("original_name", ""),
                "first_air_date": data.get("first_air_date", ""),
                "last_air_date": data.get("last_air_date", ""),
                "number_of_seasons": data.get("number_of_seasons", 0),
                "seasons": data.get("seasons", []),
                "last_episode_to_air": data.get("last_episode_to_air"),
            }
        )
    return base


async def load_cache_from_db() -> Dict[Tuple[str, int], Any]:
    """Загружает TMDB-кэш из таблицы media_cards."""
    try:
        async with async_session_maker() as db:
            result = await db.execute(select(MediaCard))
            rows = result.scalars().all()

        cache: Dict[Tuple[str, int], Any] = {}
        for mc in rows:
            key = (mc.media_type, mc.tmdb_id)
            if mc.media_type == "movie":
                cache[key] = {
                    "title": mc.title or "",
                    "original_title": mc.original_title or "",
                    "poster_path": mc.poster_path or "",
                    "backdrop_path": mc.backdrop_path or "",
                    "overview": mc.overview or "",
                    "vote_average": mc.vote_average or 0,
                    "release_date": mc.release_date or "",
                }
            else:  # tv
                seasons = []
                if mc.seasons_json:
                    try:
                        seasons = json.loads(mc.seasons_json)
                    except Exception:
                        pass
                last_ep = None
                if mc.last_ep_season and mc.last_ep_number:
                    last_ep = {
                        "season_number": mc.last_ep_season,
                        "episode_number": mc.last_ep_number,
                    }
                cache[key] = {
                    "name": mc.title or "",
                    "original_name": mc.original_title or "",
                    "poster_path": mc.poster_path or "",
                    "backdrop_path": mc.backdrop_path or "",
                    "overview": mc.overview or "",
                    "vote_average": mc.vote_average or 0,
                    "first_air_date": mc.release_date or "",
                    "last_air_date": mc.last_air_date or "",
                    "number_of_seasons": mc.number_of_seasons or 0,
                    "seasons": seasons,
                    "last_episode_to_air": last_ep,
                }
        return cache
    except Exception as e:
        logger.error(f"Ошибка загрузки TMDB кэша из БД: {e}")
        return {}


def get_quality_text(video_quality: int) -> str:
    """Возвращает текстовое описание качества"""
    quality_map = {
        (0, 99): "SD",
        100: "WEBDL 720p",
        101: "BDRip 720p",
        (102, 199): "BDRip HEVC 720p",
        200: "WEBDL 1080p",
        201: "BDRip 1080p",
        202: "BDRip HEVC 1080p",
        203: "Remux 1080p",
        (204, 299): "1080p",
        300: "WEBDL 2160p",
        301: "WEBDL HDR 2160p",
        302: "WEBDL DV 2160p",
        303: "BDRip 2160p",
        304: "BDRip HDR 2160p",
        305: "BDRip DV 2160p",
        306: "Remux 2160p",
        307: "Remux HDR 2160p",
        308: "Remux DV 2160p",
        (309, float("inf")): "2160p",
    }

    for k, v in quality_map.items():
        if isinstance(k, tuple):
            if k[0] <= video_quality <= k[1]:
                return v
        elif video_quality == k:
            return v
    return ""


def _media_card_to_lampac_item(mc: MediaCard) -> dict:
    """Convert a MediaCard ORM row to a Lampac-format API item dict."""
    item: dict = {
        "id": mc.tmdb_id,
        "media_type": mc.media_type,
        "poster_path": mc.poster_path or "",
        "backdrop_path": mc.backdrop_path or "",
        "overview": mc.overview or "",
        "vote_average": mc.vote_average or 0,
        "vote_count": mc.vote_count or 0,
        "original_language": mc.original_language or "",
        "status": mc.status or "",
        "video": False,
    }
    if mc.media_type == "tv":
        item["name"] = mc.title or ""
        item["original_name"] = mc.original_title or ""
        item["first_air_date"] = mc.first_air_date or mc.release_date or ""
        item["last_air_date"] = mc.last_air_date or ""
        item["number_of_seasons"] = mc.number_of_seasons or 0
        seasons_data = mc.seasons
        if seasons_data:
            item["seasons"] = seasons_data
        elif mc.seasons_json:
            try:
                item["seasons"] = json.loads(mc.seasons_json)
            except Exception:
                item["seasons"] = []
        else:
            item["seasons"] = []
        if mc.last_ep_season and mc.last_ep_number:
            item["last_episode_to_air"] = {
                "season_number": mc.last_ep_season,
                "episode_number": mc.last_ep_number,
            }
        else:
            item["last_episode_to_air"] = None
    else:
        item["title"] = mc.title or ""
        item["original_title"] = mc.original_title or ""
        item["release_date"] = mc.release_date or ""
    item["year"] = (mc.release_date or mc.first_air_date or "")[:4]
    if mc.best_video_quality is not None:
        item["release_quality"] = get_quality_text(mc.best_video_quality)
    return item


async def load_data_from_db(category: str, db: AsyncSession,
                            page: int = 0, per_page: int = 0,
                            search: str | None = None,
                            exclude_card_ids: set[str] | None = None) -> dict:
    """Load category data from media_cards + torrents (replaces file-based load_data)."""

    # rutor_category values written by Go parser:
    #   "Movie", "Series", "CartoonMovie", "CartoonSeries", "Anime",
    #   "DocMovie", "DocSeries", "TVShow"
    # NUMParser filterEntitiesByCategory logic reproduced:
    # Split by year:
    #   newList  — abs(torr.Year - currentYear) < yearDelta
    #              sorted by latest_torrent_date DESC
    #   allList  — older
    #              sorted by release_date DESC
    # _new categories serve newList only
    # non-_new movie categories serve allList only
    # TV/cartoon/anime serve combined (new first → torrent_date, then old → release_date)
    #
    # yearDelta=2 → cutoff = cur_year-1 (movies, tv, cartoons, anime)
    # yearDelta=4 → cutoff = cur_year-3 (4k)

    import datetime as _dt
    _cur_year = _dt.datetime.now().year
    _cutoff2 = f"{_cur_year - 1}-01-01"   # delta=2: last 2 years
    _cutoff4 = f"{_cur_year - 3}-01-01"   # delta=4: last 4 years

    rutor_cats: list[str] | None = None
    media_type_filter: str | None = None
    language: str | None = None   # "ru" | "notru" | None = any
    min_quality: int = 0
    order_by_rating = False
    year: int | None = None
    # sort_mode:
    #   "torrent"  — latest_torrent_date DESC (for _new)
    #   "release"  — release_date DESC (for old-only categories)
    #   "combined" — new first by torrent, then old by release (TV/cartoon/anime)
    #   "rating"   — vote_average DESC
    sort_mode = "torrent"
    # recency filter
    recency: str | None = None   # "new" | "old" | None
    _cutoff = _cutoff2           # default year threshold

    year_m = re.match(r"^movies_id_(\d{4})$", category)

    if category == "movies_new":
        rutor_cats, media_type_filter, language = ["Movie"], "movie", "notru"
        recency, min_quality, sort_mode = "new", 200, "torrent"
    elif category == "movies":
        rutor_cats, media_type_filter, language = ["Movie"], "movie", "notru"
        recency, min_quality, sort_mode = "old", 200, "release"
    elif category == "movies_ru_new":
        rutor_cats, media_type_filter, language = ["Movie"], "movie", "ru"
        recency, min_quality, sort_mode = "new", 200, "torrent"
    elif category == "movies_ru":
        rutor_cats, media_type_filter, language = ["Movie"], "movie", "ru"
        recency, min_quality, sort_mode = "old", 200, "release"
    elif category == "tv_shows":
        rutor_cats, media_type_filter, language = ["Series"], "tv", "notru"
        min_quality, sort_mode = 200, "torrent"
    elif category == "tv_shows_ru":
        rutor_cats, media_type_filter, language = ["Series"], "tv", "ru"
        min_quality, sort_mode = 0, "torrent"
    elif category == "movies_4k_new":
        rutor_cats, media_type_filter = ["Movie"], "movie"
        recency, min_quality, sort_mode, _cutoff = "new", 300, "torrent", _cutoff4
    elif category == "movies_4k":
        rutor_cats, media_type_filter = ["Movie"], "movie"
        recency, min_quality, sort_mode, _cutoff = "old", 300, "release", _cutoff4
    elif category == "legends_id":
        # NUMParser: TMDB movie/top_rated filtered to Cyrillic titles + have torrents
        # We approximate with weighted ranking: vote_average * ln(vote_count), min 100 votes
        media_type_filter, sort_mode, min_quality = "movie", "weighted", 200
    elif category == "cartoon_movies":
        rutor_cats, media_type_filter = ["CartoonMovie"], "movie"
        min_quality, sort_mode = 200, "combined"
    elif category == "cartoon_series":
        rutor_cats, media_type_filter = ["CartoonSeries"], "tv"
        min_quality, sort_mode = 200, "torrent"
    elif category == "anime":
        rutor_cats, media_type_filter = ["Anime"], "tv"
        min_quality, sort_mode = 0, "torrent"
    elif category == "np_popular":
        # Sort by weighted ranking across all media types
        sort_mode = "weighted"
    elif year_m:
        # NUMParser: vote_average * log(vote_count) sort for year categories
        year, media_type_filter, sort_mode = int(year_m.group(1)), "movie", "weighted"
    else:
        raise HTTPException(status_code=404, detail=f"Unknown category: {category}")

    # Filter only cards that have torrents (Go parser sets latest_torrent_date)
    q = select(MediaCard).where(MediaCard.latest_torrent_date.isnot(None))

    if rutor_cats:
        q = q.where(MediaCard.rutor_category.in_(rutor_cats))
    if min_quality > 0:
        q = q.where(MediaCard.best_video_quality >= min_quality)

    if media_type_filter:
        q = q.where(MediaCard.media_type == media_type_filter)

    if language == "ru":
        q = q.where(MediaCard.original_language == "ru")
    elif language == "notru":
        q = q.where(MediaCard.original_language != "ru")

    if recency == "new":
        q = q.where(MediaCard.release_date >= _cutoff)
    elif recency == "old":
        q = q.where(MediaCard.release_date < _cutoff)

    if year:
        q = q.where(MediaCard.release_date.like(f"{year}%"))

    # Apply sort
    from sqlalchemy import case as sa_case
    if sort_mode == "rating":
        q = q.order_by(
            MediaCard.vote_average.desc().nullslast(),
            MediaCard.vote_count.desc().nullslast(),
            MediaCard.tmdb_id.desc(),
        )
    elif sort_mode == "release":
        q = q.order_by(
            MediaCard.release_date.desc().nullslast(),
            MediaCard.tmdb_id.desc(),
        )
    elif sort_mode == "combined":
        # New items first (by torrent date), then old (by release date)
        is_new = MediaCard.release_date >= _cutoff
        q = q.order_by(
            sa_case((is_new, 0), else_=1),
            sa_case((is_new, MediaCard.latest_torrent_date)).desc().nullslast(),
            sa_case((~is_new, MediaCard.release_date)).desc().nullslast(),
            MediaCard.tmdb_id.desc(),
        )
    elif sort_mode == "weighted":
        # NUMParser formula: vote_average * ln(vote_count)
        # Requires minimum vote count to filter out low-vote noise
        from sqlalchemy import func as sa_func
        weighted = MediaCard.vote_average * sa_func.ln(
            sa_func.greatest(MediaCard.vote_count, 1).cast(Float)
        )
        q = q.where(MediaCard.vote_count >= 100)
        q = q.order_by(weighted.desc(), MediaCard.tmdb_id.desc())
    else:  # "torrent"
        q = q.order_by(
            MediaCard.latest_torrent_date.desc().nullslast(),
            MediaCard.tmdb_id.desc(),
        )

    if search:
        like = f"%{search}%"
        q = q.where(MediaCard.title.ilike(like) | MediaCard.original_title.ilike(like))

    if exclude_card_ids:
        q = q.where(MediaCard.card_id.notin_(exclude_card_ids))

    # Total count (needed for pagination metadata)
    count_q = select(func.count()).select_from(q.subquery())
    total = (await db.execute(count_q)).scalar() or 0

    if page > 0 and per_page > 0:
        q = q.offset((page - 1) * per_page).limit(per_page)

    rows = (await db.execute(q)).scalars().all()
    return {
        "results": [_media_card_to_lampac_item(mc) for mc in rows],
        "total": total,
    }


async def upsert_tmdb_cache(media_type: str, tmdb_id: int, data: dict) -> None:
    """Сохраняет TMDB-данные в media_cards (upsert)."""
    card_id = f"{tmdb_id}_{media_type}"
    if media_type == "movie":
        date_val = data.get("release_date") or ""
        values = {
            "card_id": card_id,
            "tmdb_id": tmdb_id,
            "media_type": media_type,
            "title": data.get("title") or "",
            "original_title": data.get("original_title") or "",
            "poster_path": data.get("poster_path") or "",
            "year": date_val[:4],
            "backdrop_path": data.get("backdrop_path") or "",
            "overview": data.get("overview") or "",
            "vote_average": data.get("vote_average"),
            "release_date": date_val,
            "runtime": data.get("runtime"),
        }
    else:  # tv
        date_val = data.get("first_air_date") or ""
        seasons = data.get("seasons")
        values = {
            "card_id": card_id,
            "tmdb_id": tmdb_id,
            "media_type": media_type,
            "title": data.get("name") or "",
            "original_title": data.get("original_name") or "",
            "poster_path": data.get("poster_path") or "",
            "year": date_val[:4],
            "backdrop_path": data.get("backdrop_path") or "",
            "overview": data.get("overview") or "",
            "vote_average": data.get("vote_average"),
            "release_date": date_val,
            "last_air_date": data.get("last_air_date") or "",
            "number_of_seasons": data.get("number_of_seasons"),
            "seasons_json": (
                json.dumps(seasons, ensure_ascii=False) if seasons else None
            ),
            "last_ep_season": (data.get("last_episode_to_air") or {}).get(
                "season_number"
            ),
            "last_ep_number": (data.get("last_episode_to_air") or {}).get(
                "episode_number"
            ),
            "next_ep_air_date": (data.get("next_episode_to_air") or {}).get("air_date") or "",
            "episode_run_time": (lambda ert: ert[0] if ert else 0)(data.get("episode_run_time") or []),
        }

    try:
        async with async_session_maker() as db:
            stmt = pg_insert(MediaCard).values([values])
            # Не затираем непустые поля пустыми значениями
            update_set = {
                k: stmt.excluded[k] for k in values
                if k != "card_id"
                and not (k in ("poster_path", "overview", "title") and not values[k])
            }
            stmt = stmt.on_conflict_do_update(
                index_elements=["card_id"],
                set_=update_set,
            )
            await db.execute(stmt)
            await db.commit()
    except Exception as e:
        logger.error(f"Ошибка upsert MediaCard {card_id}: {e}")


def convert_date(date_str: str) -> str:
    """Конвертирует дату из формата 'дд.мм.гггг' в 'гггг-мм-дд'"""
    try:
        return datetime.strptime(date_str, "%d.%m.%Y").strftime("%Y-%m-%d")
    except:
        try:
            return datetime.strptime(date_str, "%Y-%m-%d").strftime("%Y-%m-%d")
        except:
            return date_str


async def fetch_tmdb_batch(requests_list: list) -> dict:
    """Пакетный запрос к TMDB API"""
    results = {}

    def make_request(media_type, tmdb_id):
        try:
            url = f"https://api.themoviedb.org/3/{media_type}/{tmdb_id}"
            headers = {"Authorization": TMDB_TOKEN}
            params = {"language": "ru-RU"}
            response = requests.get(url, headers=headers, params=params, timeout=5)
            response.raise_for_status()
            return (media_type, tmdb_id), response.json()
        except Exception as e:
            logger.error(f"Ошибка запроса к TMDB {media_type}/{tmdb_id}: {str(e)}")
            return (media_type, tmdb_id), None

    # Выполняем запросы в пуле потоков
    loop = asyncio.get_running_loop()
    futures = [
        loop.run_in_executor(executor, make_request, media_type, tmdb_id)
        for media_type, tmdb_id in requests_list
    ]

    for future in asyncio.as_completed(futures):
        key, data = await future
        if data:  # Сохраняем только успешные ответы
            media_type, tmdb_id = key
            cleaned = _extract_tmdb_fields(media_type, data)
            results[key] = cleaned
            tmdb_cache[key] = cleaned
            asyncio.create_task(upsert_tmdb_cache(media_type, tmdb_id, data))

    return results


def enhance_with_tmdb(item: dict, tmdb_data: dict) -> dict:
    """Обогащает данные из файла информацией с TMDB"""
    if not tmdb_data:
        return None

    result = {
        "id": item["id"],
        "poster_path": tmdb_data.get("poster_path", ""),
        "overview": tmdb_data.get("overview", ""),
        "vote_average": tmdb_data.get("vote_average", 0),
        "backdrop_path": tmdb_data.get("backdrop_path", ""),
    }

    media_type = item.get("media_type", "movie")

    if media_type == "movie":
        result.update(
            {
                "title": tmdb_data.get("title", ""),
                "original_title": tmdb_data.get("original_title", ""),
                "release_date": convert_date(
                    item.get("release_date") or tmdb_data.get("release_date", "")
                ),
            }
        )
    else:  # tv
        result.update(
            {
                "name": tmdb_data.get("name", ""),
                "original_name": tmdb_data.get("original_name", ""),
                "first_air_date": convert_date(
                    item.get("release_date") or tmdb_data.get("first_air_date", "")
                ),
                "last_air_date": convert_date(
                    item.get("release_date") or tmdb_data.get("last_air_date", "")
                ),
                "number_of_seasons": tmdb_data.get("number_of_seasons", 0),
                "seasons": tmdb_data.get("seasons", []),
            }
        )

    if "torrent" in item:
        qualities = [
            t["quality"]
            for t in item["torrent"]
            if "quality" in t and t["quality"] is not None
        ]
        if qualities:
            result["release_quality"] = get_quality_text(max(qualities))

    return result


def get_clear_cache_password():
    """Получает пароль из переменных окружения"""
    password = get_settings().CACHE_CLEAR_PASSWORD
    if not password:
        logger.error("Пароль для очистки кэша не задан в переменных окружения")
        raise RuntimeError("Не настроен пароль для очистки кэша")
    return password


# Точные названия из np.js (getAllCategories)
_CATEGORY_TITLES: dict[str, str] = {
    "movies_ru_new":    "Новые русские фильмы",
    "movies_new":       "Новые фильмы",
    "tv_shows":         "Сериалы",
    "tv_shows_ru":      "Русские сериалы",
    "movies_4k_new":    "В высоком качестве (новые)",
    "legends_id":       "Топ фильмы",
    "movies_4k":        "В высоком качестве",
    "movies":           "Фильмы",
    "movies_ru":        "Русские фильмы",
    "cartoon_movies":   "Мультфильмы",
    "cartoon_series":   "Мультсериалы",
    "anime":            "Аниме",
    "np_popular":       "Популярно в NP",
}

_CATEGORY_ORDER: list[str] = list(_CATEGORY_TITLES.keys())


def _category_display_name(cat_id: str) -> str:
    if cat_id in _CATEGORY_TITLES:
        return _CATEGORY_TITLES[cat_id]
    m = re.match(r"^movies_id_(\d{4})$", cat_id)
    if m:
        return f"Фильмы {m.group(1)} года"
    return cat_id


def _item_card_id(item: dict) -> str | None:
    """Вычисляет card_id для элемента в формате '{tmdb_id}_{media_type}'."""
    try:
        tmdb_id = int(item.get("id", 0))
        media_type = item.get("media_type")
        if not media_type:
            # Lampac-файлы не содержат media_type — определяем по TMDB-полям:
            # сериалы имеют seasons/last_episode_to_air, фильмы — нет
            if (
                item.get("seasons") is not None
                or item.get("last_episode_to_air") is not None
            ):
                media_type = "tv"
            else:
                media_type = "movie"
        if tmdb_id:
            return f"{tmdb_id}_{media_type}"
    except (ValueError, TypeError):
        pass
    return None


def _tv_show_watched(
    item: dict,
    item_timecodes: dict[str, str],
    threshold: int | None = None,
    show_episodes: list[tuple[int, int]] | None = None,
) -> bool:
    """
    Проверяет, все ли нужные эпизоды сериала просмотрены.

    Приоритет: show_episodes из таблицы episodes (MyShows, без спешлов).
    Fallback: TMDB seasons + last_episode_to_air.
    """
    if threshold is None:
        threshold = _sc.get_int("watched_threshold")
    original_name = item.get("original_name") or item.get("original_title", "")
    if not original_name:
        return False

    # Хеши эпизодов с достаточным прогрессом
    watched_hashes: set[str] = set()
    for h, data_str in item_timecodes.items():
        try:
            if json.loads(data_str).get("percent", 0) >= threshold:
                watched_hashes.add(h)
        except (json.JSONDecodeError, TypeError):
            pass

    # Приоритет: MyShows episodes table (только вышедшие, без спешлов)
    if show_episodes:
        for sn, ep in show_episodes:
            h = lampa_hash(build_episode_hash_string(sn, ep, original_name))
            if h not in watched_hashes:
                return False
        return True

    # Fallback: TMDB seasons + last_episode_to_air
    seasons = [s for s in item.get("seasons", []) if s.get("season_number", 0) > 0]
    if not seasons:
        return False

    last_ep = item.get("last_episode_to_air")
    if last_ep:
        last_season = last_ep.get("season_number", 0)
        last_episode = last_ep.get("episode_number", 0)
        if not last_season or not last_episode:
            return False
        season_ep_count = {s["season_number"]: s["episode_count"] for s in seasons}
        for sn in range(1, last_season + 1):
            ep_count = last_episode if sn == last_season else season_ep_count.get(sn, 0)
            for ep in range(1, ep_count + 1):
                h = lampa_hash(build_episode_hash_string(sn, ep, original_name))
                if h not in watched_hashes:
                    return False
    else:
        for s in seasons:
            sn = s["season_number"]
            for ep in range(1, s.get("episode_count", 0) + 1):
                if lampa_hash(build_episode_hash_string(sn, ep, original_name)) not in watched_hashes:
                    return False

    return True


def _item_watched(
    item: dict,
    timecodes: dict[str, dict[str, str]],
    watched_movies: set[str],
    threshold: int | None = None,
    episodes_by_show: dict[int, list[tuple[int, int]]] | None = None,
) -> bool:
    """True если элемент уже полностью просмотрен и должен быть скрыт."""
    card_id = _item_card_id(item)
    if not card_id:
        return False
    if card_id.endswith("_tv"):
        if card_id not in timecodes:
            return False
        tmdb_id = item.get("id")
        show_eps = episodes_by_show.get(tmdb_id) if episodes_by_show and tmdb_id else None
        return _tv_show_watched(item, timecodes[card_id], threshold=threshold, show_episodes=show_eps)
    return card_id in watched_movies


@app.get("/health")
async def health_check():
    return {"status": "ok"}


@app.get("/imgproxy/{path:path}")
async def image_proxy(path: str):
    """Проксирует изображения TMDB через настроенный прокси-сервер."""
    if not settings.IMAGE_PROXY_URL:
        raise HTTPException(status_code=404, detail="Image proxy not configured")

    proxy_url = settings.IMAGE_PROXY_URL
    if settings.IMAGE_PROXY_USER and settings.IMAGE_PROXY_PASS:
        scheme, rest = proxy_url.split("://", 1)
        proxy_url = (
            f"{scheme}://{settings.IMAGE_PROXY_USER}:{settings.IMAGE_PROXY_PASS}@{rest}"
        )

    tmdb_url = f"https://image.tmdb.org/{path}"
    try:
        async with httpx.AsyncClient(
            proxy=proxy_url, timeout=20, follow_redirects=True
        ) as client:
            resp = await client.get(tmdb_url)
        if resp.status_code != 200:
            raise HTTPException(status_code=resp.status_code)
        return Response(
            content=resp.content,
            media_type=resp.headers.get("content-type", "image/jpeg"),
            headers={"Cache-Control": "public, max-age=604800"},
        )
    except HTTPException:
        raise
    except Exception as e:
        logger.warning(f"Image proxy error for {path}: {e}")
        raise HTTPException(status_code=502, detail="Proxy error")


_TMDB_IMAGE_PREFIX_RE = re.compile(r'^https?://[^/]*image\.tmdb\.org/t/p/\w+')

def _normalize_poster_path(path: str) -> str:
    """Возвращает только путь вида /abc.jpg, убирая базовый URL если есть."""
    if not path:
        return ""
    m = _TMDB_IMAGE_PREFIX_RE.match(path)
    if m:
        return path[m.end():]  # оставляем только /abc.jpg
    return path


@app.get("/api/search")
async def api_search(q: str = Query(..., min_length=3), db: AsyncSession = Depends(get_db)):
    """Глобальный поиск по media_cards."""
    like = f"%{q.strip()}%"

    rows = (await db.execute(
        select(MediaCard)
        .where(MediaCard.title.ilike(like) | MediaCard.original_title.ilike(like))
        .order_by(MediaCard.vote_count.desc().nullslast())
        .limit(100)
    )).scalars().all()

    results = []
    for mc in rows:
        results.append({
            "id":             mc.tmdb_id,
            "media_type":     mc.media_type,
            "title":          mc.title or "",
            "original_title": mc.original_title or "",
            "poster_path":    _normalize_poster_path(mc.poster_path or ""),
            "year":           (mc.release_date or mc.first_air_date or "")[:4],
        })

    return {"results": results, "total": len(results)}


@app.get("/api/categories")
async def api_categories(db: AsyncSession = Depends(get_db)):
    """Список категорий на основе данных в БД."""
    result: list[dict] = []

    # Виртуальная категория — только если есть просмотры
    has_popular = (await db.execute(
        select(func.count()).select_from(Timecode).where(Timecode.view_count > 0)
    )).scalar() or 0
    if has_popular:
        result.append({"id": "np_popular", "name": "Популярно в NP"})

    # Фиксированные категории (всегда доступны из БД)
    for cat_id in _CATEGORY_ORDER:
        if cat_id != "np_popular":
            result.append({"id": cat_id, "name": _category_display_name(cat_id)})

    # Годовые категории — по данным в media_cards
    yr_col = func.substr(MediaCard.release_date, 1, 4).label("yr")
    year_rows = (await db.execute(
        select(yr_col)
        .where(
            MediaCard.media_type == "movie",
            MediaCard.release_date.isnot(None),
            func.length(MediaCard.release_date) >= 4,
        )
        .group_by(yr_col)
        .order_by(yr_col.desc())
    )).all()

    current_year = datetime.now().year
    for row in year_rows:
        yr = row.yr
        if yr and yr.isdigit() and 1980 <= int(yr) <= current_year:
            cat_id = f"movies_id_{yr}"
            result.append({"id": cat_id, "name": _category_display_name(cat_id)})

    return result


@app.get("/catalog/{category}", response_class=HTMLResponse)
async def catalog_category_page(
    request: Request,
    category: str,
    current_user: User = Depends(get_current_user),
    db: AsyncSession = Depends(get_db),
):
    if not re.match(r"^[\w\-]+$", category):
        raise HTTPException(status_code=404, detail="Not found")
    image_base = "/imgproxy" if settings.IMAGE_PROXY_URL else "https://image.tmdb.org"
    _known_categories = set(_CATEGORY_TITLES.keys())
    year_cat = re.match(r"^movies_id_\d{4}$", category)
    if category not in _known_categories and not year_cat:
        raise HTTPException(status_code=404, detail="Category not found")
    devices = []
    if current_user:
        from app.api.devices import _devices_with_stats
        devices = await _devices_with_stats(current_user.id, db)
    return _templates.TemplateResponse("catalog_category.html", {
        "request": request,
        "user": current_user,
        "category": category,
        "category_name": _category_display_name(category),
        "image_base": image_base,
        "devices": devices,
    })


@app.get("/actor/{person_id}", response_class=HTMLResponse)
async def actor_page(
    person_id: int,
    request: Request,
    current_user: User = Depends(get_current_user),
):
    if not current_user:
        return RedirectResponse(url="/login", status_code=302)
    image_base = "/imgproxy" if settings.IMAGE_PROXY_URL else "https://image.tmdb.org"
    return _templates.TemplateResponse("actor.html", {
        "request": request,
        "user": current_user,
        "person_id": person_id,
        "image_base": image_base,
    })


@app.get("/history", response_class=HTMLResponse)
async def history_page(
    request: Request,
    current_user: User = Depends(get_current_user),
    db: AsyncSession = Depends(get_db),
):
    if not current_user:
        return RedirectResponse(url="/login", status_code=302)
    from app.api.devices import _devices_with_stats
    devices = await _devices_with_stats(current_user.id, db)
    image_base = "/imgproxy" if settings.IMAGE_PROXY_URL else "https://image.tmdb.org"
    return _templates.TemplateResponse("history.html", {
        "request": request,
        "user": current_user,
        "devices": devices,
        "image_base": image_base,
    })


@app.get("/card/{card_id}", response_class=HTMLResponse)
async def card_detail_page(
    request: Request,
    card_id: str,
    current_user: User = Depends(get_current_user),
):
    if not current_user:
        return RedirectResponse(url="/login", status_code=302)
    image_base = "/imgproxy" if settings.IMAGE_PROXY_URL else "https://image.tmdb.org"
    return _templates.TemplateResponse("card_detail.html", {
        "request": request,
        "user": current_user,
        "card_id": card_id,
        "image_base": image_base,
    })


@app.get("/{category}")
async def get_category(
    category: str,
    request: Request,
    page: int = 1,
    per_page: int = 20,
    language: str = "ru",
    token: str = Query(None),
    profile_id: str = Query(None),
    min_progress: int = Query(None, ge=1, le=100),
    search: str = Query(None),
    db: AsyncSession = Depends(get_db),
):
    if not re.match(r"^[\w\-]+$", category):
        raise HTTPException(status_code=404, detail="Not found")

    try:
        logger.debug(
            f"Запрос: {category}, страница {page}, token={'yes' if token else 'no'}"
        )

        # Загружаем таймкоды устройства (если передан token)
        timecodes: dict = {}
        watched_movies: set[str] = set()
        episodes_by_show: dict[int, list[tuple[int, int]]] = {}
        if token:
            device = await get_device_by_token(token=token, db=db)
            if device:
                timecodes = await load_device_timecodes(db, device.id, profile_id or "")
                watched_movies = get_watched_movie_ids(timecodes, threshold=min_progress)
                # Загружаем эпизоды из MyShows для TV-шоу (приоритет над TMDB при фильтрации)
                tv_tmdb_ids = [
                    int(k[:-3]) for k in timecodes
                    if k.endswith("_tv") and k[:-3].isdigit()
                ]
                if tv_tmdb_ids:
                    from sqlalchemy import select as _select
                    ep_rows = await db.execute(
                        _select(Episode.tmdb_show_id, Episode.season, Episode.episode)
                        .where(
                            Episode.tmdb_show_id.in_(tv_tmdb_ids),
                            Episode.is_special == False,  # noqa: E712
                            Episode.season > 0,
                            (Episode.air_date == None) | (Episode.air_date <= _date.today()),  # noqa: E711
                        )
                        .order_by(Episode.tmdb_show_id, Episode.season, Episode.episode)
                    )
                    for tid, s, e in ep_rows.all():
                        episodes_by_show.setdefault(tid, []).append((s, e))

        # ── "Продолжить просмотр" — незавершённые из таймкодов ──────────────────
        if category == "continues" or category.startswith("continues_"):
            if not token:
                return {"results": [], "page": 1, "total_pages": 1, "total_results": 0}

            media_filter = None
            if category == "continues_movie":
                media_filter = "movie"
            elif category in ("continues_tv", "continues_anime"):
                media_filter = "tv"
            # continues — без фильтра, все типы

            device = await get_device_by_token(token=token, db=db)
            if not device:
                return {"results": [], "page": 1, "total_pages": 1, "total_results": 0}

            tc_where = [Timecode.device_id == device.id]
            if profile_id is not None:
                tc_where.append(Timecode.lampa_profile_id == profile_id)
            tc_result = await db.execute(select(Timecode).where(*tc_where))
            all_tc = tc_result.scalars().all()

            # Группируем: card_id → {max_pct, last_watched, items}
            agg: dict[str, dict] = {}
            for tc in all_tc:
                if not re.match(r"^\d+_(movie|tv)$", tc.card_id):
                    continue
                if media_filter and not tc.card_id.endswith(f"_{media_filter}"):
                    continue
                try:
                    pct = float(json.loads(tc.data).get("percent", 0))
                except Exception:
                    pct = 0
                if tc.card_id not in agg:
                    agg[tc.card_id] = {"max_pct": pct, "last_watched": tc.updated_at, "items": {}}
                else:
                    if pct > agg[tc.card_id]["max_pct"]:
                        agg[tc.card_id]["max_pct"] = pct
                    if tc.updated_at and (
                        not agg[tc.card_id]["last_watched"]
                        or tc.updated_at > agg[tc.card_id]["last_watched"]
                    ):
                        agg[tc.card_id]["last_watched"] = tc.updated_at
                agg[tc.card_id]["items"][tc.item] = max(
                    agg[tc.card_id]["items"].get(tc.item, 0), pct
                )

            # Загружаем MediaCard для всех card_id (нужны seasons_json для сериалов)
            mc_all_result = await db.execute(
                select(MediaCard).where(MediaCard.card_id.in_(list(agg.keys())))
            )
            mc_map = {mc.card_id: mc for mc in mc_all_result.scalars().all()}

            today_str = _date.today().isoformat()

            def _is_unfinished(cid: str, v: dict) -> bool:
                mc = mc_map.get(cid)
                if cid.endswith("_tv") and mc and mc.seasons_json:
                    try:
                        seasons = json.loads(mc.seasons_json)
                        last_s = mc.last_ep_season or 0
                        last_e = mc.last_ep_number or 0
                        total_aired = 0
                        for s in seasons:
                            snum = s.get("season_number") or 0
                            if snum == 0:
                                continue
                            ep_count = s.get("episode_count") or 0
                            if last_s > 0:
                                if snum < last_s:
                                    total_aired += ep_count
                                elif snum == last_s:
                                    total_aired += last_e
                            else:
                                s_air = s.get("air_date") or ""
                                if s_air and s_air <= today_str:
                                    total_aired += ep_count
                        _thr = min_progress if min_progress is not None else _sc.get_int("watched_threshold")
                        watched = sum(1 for p in v["items"].values() if p >= _thr)
                        return watched < total_aired
                    except Exception:
                        pass
                _thr = min_progress if min_progress is not None else _sc.get_int("watched_threshold")
                return v["max_pct"] < _thr

            unfinished = [
                (cid, v["max_pct"], v["last_watched"])
                for cid, v in agg.items()
                if _is_unfinished(cid, v)
            ]
            unfinished.sort(key=lambda x: x[2] or datetime.min, reverse=True)

            total = len(unfinished)
            start = (page - 1) * per_page
            page_items = unfinished[start : start + per_page]

            if not page_items:
                return {
                    "results": [],
                    "page": page,
                    "total_pages": ceil(total / per_page) or 1,
                    "total_results": total,
                }

            # mc_map уже загружен выше

            results = []
            for cid, pct, _ in page_items:
                mc = mc_map.get(cid)
                if not mc:
                    continue
                item: dict = {
                    "id": mc.tmdb_id,
                    "poster_path": mc.poster_path,
                    "backdrop_path": mc.backdrop_path or "",
                    "overview": mc.overview or "",
                    "vote_average": mc.vote_average or 0,
                }
                if mc.media_type == "tv":
                    item["name"] = mc.title
                    item["original_name"] = mc.original_title
                    item["first_air_date"] = mc.release_date or ""
                    item["media_type"] = "tv"
                else:
                    item["title"] = mc.title
                    item["original_title"] = mc.original_title
                    item["release_date"] = mc.release_date or ""
                    item["media_type"] = "movie"
                item["year"] = (mc.release_date or mc.first_air_date or "")[:4]
                results.append(item)

            return {
                "results": results,
                "page": page,
                "total_pages": ceil(total / per_page) or 1,
                "total_results": total,
            }

        # ── "Популярно в NP" — глобальный рейтинг просмотров ───────────────────
        if category == "np_popular":
            from datetime import timedelta
            from app.api.timecodes import _media_card_to_entry
            period = _sc.get_int("popular_period_days") or 30
            cutoff = _date.today() - timedelta(days=period)

            # Реальное кол-во серий: episodes (без спецвыпусков) → media_cards → COUNT(DISTINCT item)
            ep_count_sq = (
                select(Episode.tmdb_show_id, func.count().label("n_ep"))
                .where(Episode.is_special == False)
                .group_by(Episode.tmdb_show_id)
            ).subquery()
            # Фолбэк 3: COUNT(DISTINCT item) — уникальные серии из самих timecodes
            actual_n_ep = func.count(func.distinct(Timecode.item))
            effective_n_ep = func.coalesce(
                ep_count_sq.c.n_ep,
                MediaCard.number_of_episodes,
                actual_n_ep,
            )

            # Вес карточки: для фильмов = SUM(view_count),
            # для сериалов = SUM(view_count) / effective_n_ep
            _weight = case(
                (MediaCard.media_type == "movie",
                 func.sum(Timecode.view_count).cast(Float)),
                else_=(func.sum(Timecode.view_count).cast(Float)
                       / func.nullif(effective_n_ep, 0)),
            )
            weight_expr = _weight.label("weight")

            pop_filter = [
                Timecode.view_count > 0,
                Timecode.counted_at >= cutoff,
            ]
            if search:
                like = f"%{search}%"
                pop_filter.append(
                    MediaCard.title.ilike(like) | MediaCard.original_title.ilike(like)
                )
            base_q = (
                select(Timecode.card_id, weight_expr)
                .join(MediaCard, MediaCard.card_id == Timecode.card_id)
                .outerjoin(ep_count_sq, ep_count_sq.c.tmdb_show_id == MediaCard.tmdb_id)
                .where(*pop_filter)
                .group_by(
                    Timecode.card_id, MediaCard.media_type,
                    ep_count_sq.c.n_ep, MediaCard.number_of_episodes,
                )
                .having(_weight > 0)
                .order_by(_weight.desc(), func.max(Timecode.counted_at).desc())
            )
            total_pop = (
                await db.execute(select(func.count()).select_from(base_q.subquery()))
            ).scalar() or 0
            pop_rows = (
                await db.execute(
                    base_q.offset((page - 1) * per_page).limit(per_page)
                )
            ).fetchall()
            pop_ids = [r.card_id for r in pop_rows]
            mc_map_pop = {
                mc.card_id: mc
                for mc in (
                    await db.execute(select(MediaCard).where(MediaCard.card_id.in_(pop_ids)))
                ).scalars().all()
            } if pop_ids else {}
            pop_results = []
            for r in pop_rows:
                mc = mc_map_pop.get(r.card_id)
                if mc:
                    entry = _media_card_to_entry(mc)
                    entry["_np_views"] = round(r.weight, 2)
                    pop_results.append(entry)
            return {
                "results": pop_results,
                "page": page,
                "total_pages": ceil(total_pop / per_page) if total_pop else 1,
                "total_results": total_pop,
            }

        stats.track_api_user(request)
        stats.track_category_request(request, category)

        # ── Вычисляем просмотренные card_id и фильтруем в SQL ───────────────
        exclude_ids: set[str] = set()
        if timecodes or watched_movies:
            exclude_ids |= watched_movies
            tv_excluded = get_watched_tv_ids(timecodes, episodes_by_show, threshold=min_progress)
            exclude_ids |= tv_excluded

        data = await load_data_from_db(category, db,
                                       page=page, per_page=per_page,
                                       search=search,
                                       exclude_card_ids=exclude_ids or None)
        total = data["total"]
        return {
            "page": page,
            "results": data["results"],
            "total_pages": ceil(total / per_page) if per_page else 1,
            "total_results": total,
        }

    except Exception as e:
        logger.error(f"Ошибка: {str(e)}", exc_info=True)
        return JSONResponse(
            status_code=500, content={"error": "Внутренняя ошибка сервера"}
        )


_templates = get_templates()


@app.get("/", response_class=HTMLResponse)
async def index(
    request: Request,
    current_user: User = Depends(get_current_user),
    db: AsyncSession = Depends(get_db),
):
    image_base = "/imgproxy" if settings.IMAGE_PROXY_URL else "https://image.tmdb.org"
    if current_user:
        from app.api.devices import _devices_with_stats
        devices = await _devices_with_stats(current_user.id, db)
        return _templates.TemplateResponse("catalog.html", {
            "request": request,
            "user": current_user,
            "image_base": image_base,
            "devices": devices,
        })
    plugin_url = settings.PLUGIN_URL or f"{settings.BASE_URL}/np.js"
    from app import settings_cache as sc
    return _templates.TemplateResponse(
        "index.html",
        {
            "request": request,
            "user": None,
            "devices": [],
            "plugin_url": plugin_url,
            "bot_name": settings.TELEGRAM_BOT_NAME,
            "image_base": image_base,
            "simple_device_limit":   sc.get_int("simple_device_limit"),
            "premium_device_limit":  sc.get_int("premium_device_limit"),
            "simple_tc_limit":       sc.get_int("simple_timecode_limit"),
            "premium_tc_limit":      sc.get_int("premium_timecode_limit"),
            "inactive_delete_days":  sc.get_int("inactive_delete_days"),
            "inactive_warn_days":    sc.get_int("inactive_warn_days"),
        },
    )


@app.get("/cache/path")
async def get_cache_path():
    """Возвращает информацию об источнике TMDB-кэша"""
    return {
        "source": "PostgreSQL (media_cards table)",
        "cache_size": len(tmdb_cache),
    }


@app.post("/cache/clear")
async def clear_cache(x_password: str = Header(..., alias="X-Password")):
    """Очистка in-memory кэша с проверкой пароля"""
    correct_password = get_settings().CACHE_CLEAR_PASSWORD

    if not correct_password or x_password != correct_password:
        return PlainTextResponse(
            "Неверный пароль для очистки кэша\n", status_code=status.HTTP_403_FORBIDDEN
        )

    global tmdb_cache
    tmdb_cache = {}

    return PlainTextResponse("Кэш успешно очищен\n", status_code=200)


@app.get("/cache/info")
async def cache_info():
    """Возвращает информацию о кэше"""
    return {
        "cache_size": len(tmdb_cache),
        "source": "PostgreSQL",
        "sample_keys": [f"{k[0]}_{k[1]}" for k in list(tmdb_cache.keys())[:5]],
    }


async def resolve_redirects(url: str, client: httpx.AsyncClient):
    """Рекурсивно разрешаем редиректы, пока не получим конечный URL"""
    max_redirects = 5
    current_url = url
    for _ in range(max_redirects):
        try:
            response = await client.head(current_url, follow_redirects=False)
            if response.status_code in (301, 302, 303, 307, 308):
                location = response.headers.get("location")
                if location:
                    current_url = location
                    continue
            break
        except Exception:
            break
    return current_url


@app.get("/proxy/m3u")
async def proxy_m3u(url: str, request: Request):
    """
    Прокси для загрузки M3U плейлистов с обработкой коротких ссылок
    """
    if not url:
        raise HTTPException(status_code=400, detail="URL parameter is required")

    try:
        headers = {
            "User-Agent": request.headers.get("User-Agent", "Mozilla/5.0"),
            "Accept": "*/*",
        }

        async with httpx.AsyncClient(timeout=30.0) as client:
            # Сначала разрешаем редиректы
            final_url = await resolve_redirects(url, client)
            logger.info(f"Original URL: {url}, Final URL: {final_url}")

            # Затем загружаем контент
            response = await client.get(
                final_url, headers=headers, follow_redirects=True
            )

            if response.status_code != 200:
                raise HTTPException(
                    status_code=response.status_code,
                    detail=f"Failed to fetch playlist (status: {response.status_code})",
                )

            content = response.text
            if not content.lstrip().upper().startswith("#EXTM3U"):
                logger.error(f"Invalid M3U content from URL: {final_url}")
                raise HTTPException(
                    status_code=400,
                    detail="The provided URL does not point to a valid M3U playlist",
                )

            return PlainTextResponse(content=content, media_type="audio/x-mpegurl")

    except httpx.TimeoutException:
        logger.error(f"Timeout while fetching playlist from {url}")
        raise HTTPException(status_code=504, detail="Request timeout")
    except httpx.RequestError as e:
        logger.error(f"Error fetching playlist: {str(e)}")
        raise HTTPException(
            status_code=502, detail=f"Failed to fetch playlist: {str(e)}"
        )
    except Exception as e:
        logger.error(f"Unexpected error: {str(e)}")
        raise HTTPException(status_code=500, detail="Internal server error")


# Запуск сервера (для тестирования)
if __name__ == "__main__":
    import uvicorn

    uvicorn.run(app, host="0.0.0.0", port=8000)
