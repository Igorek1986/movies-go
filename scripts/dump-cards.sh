#!/usr/bin/env bash
# PUBLIC-SAFE dump: контент карточек (media_cards, torrents, episodes) +
# неприватные метки последнего парсинга (*_last_parsed_at).
#
# В отличие от dump.sh, здесь НЕТ полного app_settings — там лежат секреты
# (telegram_bot_token, kinozal_login/password, аналитика). Берём из app_settings
# ТОЛЬКО ключи *_last_parsed_at — они не приватные и нужны, чтобы на чужой машине
# парсер продолжил с момента дампа, а не сканировал каталог заново.
#
# Пользовательских данных (users, devices/токены, timecodes, myshows_*,
# telegram_*) тут нет ни в каком виде. Безопасно публиковать в GitHub Release.
#
# Usage: ./scripts/dump-cards.sh [output_file]
set -euo pipefail

OUT="${1:-cards-dump.sql.gz}"
DB_CONTAINER="${DB_CONTAINER:-movies-api-db}"
DB_USER="${DB_USER:-movies_api}"
DB_NAME="${DB_NAME:-movies_api}"

echo "Dumping cards (media_cards, torrents, episodes) + parser timestamps → $OUT ..."
{
  docker exec "$DB_CONTAINER" pg_dump -U "$DB_USER" "$DB_NAME" \
    --data-only \
    -t media_cards \
    -t torrents \
    -t episodes

  # Только метки парсинга из app_settings — как идемпотентные upsert'ы.
  echo ""
  echo "-- parser timestamps (non-private) — so a restored DB continues instead of re-scanning"
  docker exec "$DB_CONTAINER" psql -U "$DB_USER" "$DB_NAME" -tAc \
    "SELECT format(
       'INSERT INTO app_settings(key,value,updated_at) VALUES (%L,%L,%L) ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value, updated_at=EXCLUDED.updated_at;',
       key, value, updated_at)
     FROM app_settings WHERE key LIKE '%last_parsed_at';"
} | gzip > "$OUT"

echo "Done: $(du -h "$OUT" | cut -f1)"
