#!/usr/bin/env bash
# Daily public cards backup → rolling GitHub Release "cards-db".
#
# Один постоянный релиз с тегом cards-db, ассет которого перезаписывается
# каждый день. Не плодит теги. Запускать по cron НА СЕРВЕРЕ, где живёт БД
# (облачный CI до базы не достучится).
#
# Cron (каждый день в 04:00):
#   0 4 * * * cd /path/to/movies-go && ./scripts/publish-cards-dump.sh >> /var/log/cards-dump.log 2>&1
#
# Requirements: установлен и авторизован `gh`, поднята БД в докере.
set -euo pipefail

TAG="cards-db"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

./scripts/dump-cards.sh cards-dump.sql.gz

NOTES="Ежедневный авто-дамп контента карточек (media_cards, torrents, episodes).
Без пользовательских данных и секретов. Обновляется автоматически.
Последнее обновление: $(date -u '+%Y-%m-%d %H:%M UTC')"

if gh release view "$TAG" >/dev/null 2>&1; then
  gh release upload "$TAG" cards-dump.sql.gz --clobber
  gh release edit "$TAG" --notes "$NOTES"
else
  gh release create "$TAG" cards-dump.sql.gz \
    --title "Cards DB (latest)" \
    --notes "$NOTES" \
    --latest=false
fi

echo "✓ Published cards dump to release '$TAG'"
