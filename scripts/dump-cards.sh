#!/usr/bin/env bash
# PUBLIC-SAFE dump: ТОЛЬКО контент карточек (media_cards, torrents, episodes).
#
# В отличие от dump.sh, здесь НЕТ app_settings — там лежат секреты
# (telegram_bot_token, kinozal_login/password, аналитика). Этот дамп
# безопасно публиковать в открытый GitHub Release.
#
# Пользовательских данных (users, devices/токены, timecodes, myshows_*,
# telegram_*) тут нет ни в каком виде.
#
# Usage: ./scripts/dump-cards.sh [output_file]
set -euo pipefail

OUT="${1:-cards-dump.sql.gz}"
DB_CONTAINER="${DB_CONTAINER:-movies-api-db}"
DB_USER="${DB_USER:-movies_api}"
DB_NAME="${DB_NAME:-movies_api}"

echo "Dumping cards (media_cards, torrents, episodes) → $OUT ..."
docker exec "$DB_CONTAINER" pg_dump -U "$DB_USER" "$DB_NAME" \
  --data-only \
  -t media_cards \
  -t torrents \
  -t episodes \
  | gzip > "$OUT"

echo "Done: $(du -h "$OUT" | cut -f1)"
