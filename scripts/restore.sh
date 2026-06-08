#!/usr/bin/env bash
# Setup: restore dump if needed, then start the app.
# Usage: ./scripts/restore.sh
set -e

DB_USER="${DB_USER:-movies_api}"
DB_NAME="${DB_NAME:-movies_api}"

cd "$(dirname "$0")/.."

# ── 1. .env ───────────────────────────────────────────────────────────────────
if [ ! -f .env ]; then
  if [ -f .env.example ]; then
    cp .env.example .env
    echo ".env created from .env.example — fill in the values and re-run."
    exit 1
  else
    echo ".env not found. Create it before running setup."
    exit 1
  fi
fi

# ── 2. Start DB ───────────────────────────────────────────────────────────────
echo "Starting database..."
docker compose up -d db

echo "Waiting for database..."
until docker compose exec -T db pg_isready -U "$DB_USER" -d "$DB_NAME" -q 2>/dev/null; do
  sleep 2
done

# ── 3. Restore dump if media_cards is empty ───────────────────────────────────
# Prefer the public cards dump (cards-dump.sql.gz); fall back to the full local dump.
DUMP_FILE=""
for f in cards-dump.sql.gz dump.sql.gz; do
  if [ -f "$f" ]; then DUMP_FILE="$f"; break; fi
done

if [ -n "$DUMP_FILE" ]; then
  # Apply schema first so media_cards table exists before we count rows.
  docker compose exec -T db psql -U "$DB_USER" "$DB_NAME" -q < db/postgres/schema.sql 2>/dev/null || true

  COUNT=$(docker compose exec -T db psql -U "$DB_USER" "$DB_NAME" -t \
    -c "SELECT COUNT(*) FROM media_cards;" 2>/dev/null | tr -d ' \n')
  COUNT="${COUNT:-0}"

  if [ "$COUNT" = "0" ]; then
    echo "Restoring $DUMP_FILE ($(du -h "$DUMP_FILE" | cut -f1))..."
    # Strip \restrict/\unrestrict lines added by newer pg_dump versions —
    # they break psql when running non-interactively via docker exec.
    # Strip the search_path reset so unqualified table names in trailing INSERTs
    # (parser timestamps) resolve to public — robust for older dumps too.
    gunzip -c "$DUMP_FILE" \
      | grep -v '^\\\(restrict\|unrestrict\)' \
      | grep -v "set_config('search_path', '', false)" \
      | docker compose exec -T db psql -U "$DB_USER" "$DB_NAME" -q
    echo "Restore complete."
  else
    echo "media_cards already has $COUNT rows — skipping restore."
  fi
else
  echo "No cards-dump.sql.gz / dump.sql.gz — starting with empty database."
fi

# ── 4. Start app ──────────────────────────────────────────────────────────────
echo "Starting application..."
docker compose up -d --build app
# Drop the now-dangling old image left by the rebuild (only <none>, safe).
docker image prune -f >/dev/null 2>&1 || true
echo "Done."
