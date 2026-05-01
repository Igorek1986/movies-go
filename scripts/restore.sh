#!/usr/bin/env bash
# Setup: restore dump if needed, then start the app.
# Usage: ./scripts/restore.sh
set -e

DB_USER="${DB_USER:-lampa}"
DB_NAME="${DB_NAME:-lampa}"

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
if [ -f dump.sql.gz ]; then
  COUNT=$(docker compose exec -T db psql -U "$DB_USER" "$DB_NAME" -t \
    -c "SELECT COUNT(*) FROM media_cards;" 2>/dev/null | tr -d ' \n' || echo "0")

  if [ "$COUNT" = "0" ]; then
    echo "Restoring dump.sql.gz ($(du -h dump.sql.gz | cut -f1))..."
    zcat dump.sql.gz | docker compose exec -T db psql -U "$DB_USER" "$DB_NAME" -q
    echo "Restore complete."
  else
    echo "media_cards already has $COUNT rows — skipping restore."
  fi
else
  echo "No dump.sql.gz — starting with empty database."
fi

# ── 4. Start app ──────────────────────────────────────────────────────────────
echo "Starting application..."
docker compose up -d --build app
echo "Done."
