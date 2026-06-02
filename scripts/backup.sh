#!/usr/bin/env bash
# FULL database backup (ВСЯ БД, включая пользователей, токены, настройки) —
# для переезда на другой сервер.
#
# ⚠️ Содержит ВСЁ, включая секреты и данные пользователей. Храни приватно,
# НИКОГДА не публикуй (для публичного шаринга карточек есть dump-cards.sh).
#
# Usage: ./scripts/backup.sh [output_file]
set -euo pipefail

OUT="${1:-full-backup.sql.gz}"
DB_CONTAINER="${DB_CONTAINER:-movies-api-db}"
DB_USER="${DB_USER:-movies_api}"
DB_NAME="${DB_NAME:-movies_api}"

echo "Full dump of $DB_NAME → $OUT ..."
docker exec "$DB_CONTAINER" pg_dump -U "$DB_USER" "$DB_NAME" --clean --if-exists \
  | gzip > "$OUT"

echo "Done: $(du -h "$OUT" | cut -f1)"
