#!/usr/bin/env bash
# Create a data-only dump of global tables (media_cards, torrents, episodes).
# Usage: ./scripts/dump.sh [output_file]
set -e

OUT="${1:-dump.sql.gz}"
DB_CONTAINER="${DB_CONTAINER:-movies-api-db}"
DB_USER="${DB_USER:-movies_api}"
DB_NAME="${DB_NAME:-movies_api}"

echo "Dumping $DB_NAME → $OUT ..."
docker exec "$DB_CONTAINER" pg_dump -U "$DB_USER" "$DB_NAME" \
  --data-only \
  -t media_cards \
  -t torrents \
  -t episodes \
  -t app_settings \
  | gzip > "$OUT"

echo "Done: $(du -h "$OUT" | cut -f1)"
