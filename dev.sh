#!/usr/bin/env bash
# dev.sh — запуск lampa-api в режиме разработки.
# Поднимает PostgreSQL + FastAPI через docker compose, затем собирает и запускает Go парсер.
set -euo pipefail

DB_USER="lampa"
DB_PASS="lampa"
DB_NAME="lampa"
DB_PORT="5432"   # порт postgres на localhost (docker compose пробрасывает 127.0.0.1:5432)
API_PORT="${LAMPA_PORT:-8888}"

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; NC='\033[0m'
info() { echo -e "${CYAN}[lampa]${NC} $*"; }
ok()   { echo -e "${GREEN}[lampa]${NC} $*"; }
warn() { echo -e "${YELLOW}[lampa]${NC} $*"; }
die()  { echo -e "${RED}[lampa]${NC} $*" >&2; exit 1; }

# ── 1. Docker ─────────────────────────────────────────────────────────────────
docker info >/dev/null 2>&1 || die "Docker не запущен. Запусти Docker Desktop и повтори."

# ── 2. .env ───────────────────────────────────────────────────────────────────
[ -f .env ] || die ".env не найден. Скопируй и заполни по образцу из CLAUDE.md."
ok ".env найден"

# ── 3. db + api через docker compose ──────────────────────────────────────────
info "Запускаю db + api..."
docker compose up -d --build db api

# ── 4. Ждём FastAPI (схема БД создаётся при старте) ──────────────────────────
info "Жду готовности FastAPI..."
for i in $(seq 1 60); do
    if curl -sf "http://localhost:${API_PORT}/health" >/dev/null 2>&1; then
        ok "FastAPI готов — http://localhost:${API_PORT}"
        break
    fi
    if [ "$i" -eq 60 ]; then
        die "FastAPI не поднялся за 2 минуты. Проверь: docker compose logs api"
    fi
    sleep 2
done

# ── 5. config.yml ─────────────────────────────────────────────────────────────
if [ ! -f config.yml ]; then
    warn "config.yml не найден, создаю из шаблона..."
    TMDB_TOKEN="${TMDB_TOKEN:-}"
    sed \
        -e "s|postgres://user:password@localhost:5432/lampa?sslmode=disable|postgres://${DB_USER}:${DB_PASS}@localhost:${DB_PORT}/${DB_NAME}?sslmode=disable|g" \
        -e "s|tmdbtoken: \"\"|tmdbtoken: \"${TMDB_TOKEN}\"|g" \
        config.yml.example > config.yml
    ok "config.yml создан"
else
    ok "config.yml уже есть"
fi

# ── 6. Сборка парсера ─────────────────────────────────────────────────────────
info "Сборка Go парсера..."
go build -o ./lampa-parser ./cmd/main.go || die "Сборка провалилась"
ok "Собрано"

# ── 7. Запуск парсера ─────────────────────────────────────────────────────────
export DATABASE_URL="postgres://${DB_USER}:${DB_PASS}@localhost:${DB_PORT}/${DB_NAME}?sslmode=disable"
ok "Запускаю парсер..."
echo ""
exec ./lampa-parser
