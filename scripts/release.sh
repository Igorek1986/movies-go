#!/usr/bin/env bash
# Cut a release: tag main, push, create the GitHub Release and attach the
# public-safe cards dump.
#
#   ./scripts/release.sh 1.0.0
#
# Requirements: на ветке main, чистое дерево, установлен `gh` (авторизован),
# поднята БД в докере (для dump-cards.sh).
set -euo pipefail

VERSION="${1:?usage: ./scripts/release.sh X.Y.Z}"
TAG="v$VERSION"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

# --- safety checks ----------------------------------------------------------
[ "$(git symbolic-ref --short HEAD)" = "main" ] || {
  echo "✗ Не на ветке main. Сначала: git checkout main && git merge --ff-only dev"; exit 1; }
[ -z "$(git status --porcelain)" ] || { echo "✗ Рабочее дерево не чистое."; exit 1; }
git rev-parse "$TAG" >/dev/null 2>&1 && { echo "✗ Тег $TAG уже существует."; exit 1; }

# --- tag & push -------------------------------------------------------------
git tag -a "$TAG" -m "$TAG"
git push origin main
git push origin "$TAG"          # триггерит .github/workflows/release.yml (Docker → GHCR)

# --- public-safe cards dump -------------------------------------------------
./scripts/dump-cards.sh cards-dump.sql.gz

# --- create the Release with auto-generated notes + dump asset --------------
gh release create "$TAG" cards-dump.sql.gz \
  --title "$TAG" \
  --generate-notes

echo "✓ Released $TAG"
