#!/usr/bin/env bash
# Restore dump into a running DB container (only if media_cards is empty).
# Usage: ./scripts/restore.sh [dump_file]
set -e

DUMP="${1:-dump.sql.gz}"
DB_CONTAINER="${DB_CONTAINER:-lampa-api-db-1}"
DB_USER="${DB_USER:-lampa}"
DB_NAME="${DB_NAME:-lampa}"

if [ ! -f "$DUMP" ]; then
  echo "Dump file not found: $DUMP"
  exit 1
fi

COUNT=$(docker exec "$DB_CONTAINER" psql -U "$DB_USER" "$DB_NAME" -t -c \
  "SELECT COUNT(*) FROM media_cards;" 2>/dev/null | tr -d ' \n')

if [ "$COUNT" != "0" ]; then
  echo "media_cards already has $COUNT rows — skipping restore."
  exit 0
fi

echo "Restoring $DUMP → $DB_NAME ..."
zcat "$DUMP" | docker exec -i "$DB_CONTAINER" psql -U "$DB_USER" "$DB_NAME" -q
echo "Done."
