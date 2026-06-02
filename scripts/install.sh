#!/usr/bin/env bash
# Установка lampa-api: проверка зависимостей, .env, опциональный дамп карточек,
# сборка и запуск. Безопасно запускать повторно.
#
#   ./scripts/install.sh
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

REPO="Igorek1986/movies-go"
DUMP_URL="https://github.com/$REPO/releases/latest/download/cards-dump.sql.gz"

say() { printf '\n\033[1m%s\033[0m\n' "$*"; }

# ── 1. Зависимости ────────────────────────────────────────────────────────────
say "Проверка зависимостей…"
command -v docker >/dev/null 2>&1 || { echo "✗ Docker не установлен: https://docs.docker.com/get-docker/"; exit 1; }
docker compose version >/dev/null 2>&1 || { echo "✗ Нужен Docker Compose v2 (docker compose)."; exit 1; }
echo "✓ Docker и Docker Compose на месте"

# ── 2. .env ───────────────────────────────────────────────────────────────────
if [ ! -f .env ]; then
  cp .env.example .env
  say ".env создан из .env.example."
  echo "Заполни значения (минимум TMDB_TOKEN и SUPERUSER_*), затем запусти скрипт снова:"
  echo "  nano .env && ./scripts/install.sh"
  exit 0
fi
echo "✓ .env на месте"

# ── 3. Опциональный дамп карточек ─────────────────────────────────────────────
if [ ! -f cards-dump.sql.gz ] && [ ! -f dump.sql.gz ]; then
  say "Залить готовый дамп карточек, чтобы не парсить каталог с нуля?"
  read -rp "Скачать дамп из последнего релиза? [y/N] " ans
  if [[ "${ans:-}" =~ ^[Yy]$ ]]; then
    echo "Скачивание дампа…"
    if curl -fL -o cards-dump.sql.gz "$DUMP_URL"; then
      echo "✓ Дамп скачан ($(du -h cards-dump.sql.gz | cut -f1))"
    else
      echo "⚠ Не удалось скачать дамп — продолжаем с пустой базой."
      rm -f cards-dump.sql.gz
    fi
  fi
fi

# ── 4. Восстановление + запуск ────────────────────────────────────────────────
say "Восстановление базы и запуск сервиса…"
./scripts/restore.sh

PORT="$(grep -E '^PORT=' .env | tail -1 | cut -d= -f2)"
PORT="${PORT:-8888}"
say "Готово ✓  Сервис доступен на http://localhost:$PORT"
