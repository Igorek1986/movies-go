#!/usr/bin/env bash
# Clear content tables (media_cards, torrents, episodes) and reset parser
# timestamps. User data (users, devices, timecodes, myshows, telegram) is
# preserved.
#
# Usage: ./scripts/clean.sh
set -e

DB_CONTAINER="${DB_CONTAINER:-movies-api-db}"
DB_USER="${DB_USER:-movies_api}"
DB_NAME="${DB_NAME:-movies_api}"

echo "Clearing content tables in $DB_NAME (user data preserved)..."

docker exec -i "$DB_CONTAINER" psql -U "$DB_USER" "$DB_NAME" <<'SQL'
BEGIN;

-- Content tables (media_cards CASCADE drops stats_category_requests rows too)
TRUNCATE media_cards CASCADE;
TRUNCATE torrents;
TRUNCATE episodes;

-- Reset parser timestamps so next run does a full scan
DELETE FROM app_settings
WHERE key IN (
  'kinozal_last_parsed_at',
  'nnmclub_last_parsed_at',
  'rutor_last_parsed_at'
);

COMMIT;

SELECT
  (SELECT COUNT(*) FROM media_cards) AS media_cards,
  (SELECT COUNT(*) FROM torrents)    AS torrents,
  (SELECT COUNT(*) FROM episodes)    AS episodes;
SQL

echo "Done."
