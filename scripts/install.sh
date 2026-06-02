#!/usr/bin/env bash
# Интерактивная установка / обновление movies-api.
#
#   ./scripts/install.sh
#
# Проверяет зависимости, определяет уже установленный сервис (обновить/удалить),
# при свежей установке спрашивает логин/пароль администратора и TMDB-токен,
# по желанию заливает дамп карточек, запускает сервис и печатает итог.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

REPO="Igorek1986/movies-go"
DUMP_URL="https://github.com/$REPO/releases/latest/download/cards-dump.sql.gz"

say()  { printf '\n\033[1m%s\033[0m\n' "$*"; }
ok()   { printf '\033[32m✓\033[0m %s\n' "$*"; }
warn() { printf '\033[33m⚠\033[0m %s\n' "$*"; }
err()  { printf '\033[31m✗\033[0m %s\n' "$*"; }

env_val() { grep -E "^$1=" .env 2>/dev/null | tail -1 | cut -d= -f2- | tr -d ' '; }
db_user() { local v; v="$(env_val DB_USER)"; echo "${v:-movies_api}"; }
db_name() { local v; v="$(env_val DB_NAME)"; echo "${v:-movies_api}"; }
http_port() { local v; v="$(env_val PORT)"; echo "${v:-8888}"; }

# ── Зависимости ───────────────────────────────────────────────────────────────
say "Проверка зависимостей…"
command -v docker >/dev/null 2>&1 || { err "Docker не установлен: https://docs.docker.com/get-docker/"; exit 1; }
docker compose version >/dev/null 2>&1 || { err "Нужен Docker Compose v2 (docker compose)."; exit 1; }
ok "Docker и Docker Compose на месте"

# ── Уже установлено? → обновить / удалить ─────────────────────────────────────
if [ -f .env ] || docker ps -a --format '{{.Names}}' | grep -qx movies-api; then
  say "movies-api уже установлен. Что сделать?"
  echo "  1) Обновить — подтянуть код и пересобрать (данные сохранятся)"
  echo "  2) Удалить  — остановить и удалить контейнеры и базу (ВСЕ данные!)"
  echo "  3) Отмена"
  read -rp "Выбор [1/2/3]: " choice
  case "${choice:-3}" in
    1)
      say "Обновление…"
      if [ -d .git ] && git diff --quiet && git diff --cached --quiet; then
        git pull --ff-only || warn "git pull не удался — собираю текущий код"
      fi
      docker compose up -d --build
      ok "Обновлено. Адрес: http://localhost:$(http_port)"
      exit 0 ;;
    2)
      read -rp "Точно удалить контейнеры и БД? Данные не восстановить. [y/N] " c
      if [[ "${c:-}" =~ ^[Yy]$ ]]; then
        docker compose down -v
        ok "Удалено (.env оставлен). Для чистой установки запусти скрипт снова."
      else
        echo "Отменено."
      fi
      exit 0 ;;
    *) echo "Отменено."; exit 0 ;;
  esac
fi

# ── Свежая установка: интерактивный .env ──────────────────────────────────────
say "Настройка"
read -rp "Логин администратора [admin]: " SU_USER
SU_USER="${SU_USER:-admin}"

while :; do
  read -rsp "Пароль администратора: " SU_PASS; echo
  [ -n "$SU_PASS" ] || { warn "Пароль обязателен."; continue; }
  read -rsp "Повторите пароль: " SU_PASS2; echo
  [ "$SU_PASS" = "$SU_PASS2" ] || { warn "Пароли не совпадают."; continue; }
  break
done

echo
echo "TMDB токен (v4 Read Access Token) — нужен для метаданных. Можно оставить пустым."
read -rp "TMDB токен: " TMDB
if [ -n "$TMDB" ] && [[ "$TMDB" != Bearer* ]]; then
  TMDB="Bearer $TMDB"
fi

umask 077
cat > .env <<EOF
# Создано scripts/install.sh
SUPERUSER_USERNAME=$SU_USER
SUPERUSER_PASSWORD=$SU_PASS
TMDB_TOKEN=$TMDB

# PORT=8888
# DB_USER=movies_api
# DB_PASSWORD=movies_api
# DB_NAME=movies_api
EOF
ok ".env создан"
[ -n "$TMDB" ] || warn "TMDB токен пуст — обогащение метаданными выключено (задай TMDB_TOKEN в .env позже)."

# ── Дамп карточек ─────────────────────────────────────────────────────────────
if [ ! -f cards-dump.sql.gz ] && [ ! -f dump.sql.gz ]; then
  say "Залить готовый дамп карточек, чтобы не парсить каталог с нуля?"
  read -rp "Скачать дамп из последнего релиза? [Y/n] " ans
  if [[ ! "${ans:-Y}" =~ ^[Nn]$ ]]; then
    echo "Скачивание дампа…"
    if curl -fL -o cards-dump.sql.gz "$DUMP_URL"; then
      ok "Дамп скачан ($(du -h cards-dump.sql.gz | cut -f1))"
    else
      warn "Не удалось скачать дамп — продолжаю с пустой базой."
      rm -f cards-dump.sql.gz
    fi
  fi
fi

# ── Восстановление + запуск ───────────────────────────────────────────────────
say "Восстановление базы и запуск сервиса…"
./scripts/restore.sh

# ── Итог ──────────────────────────────────────────────────────────────────────
PORT="$(http_port)"
MODE="$(docker compose exec -T db psql -U "$(db_user)" "$(db_name)" -tAc \
  "SELECT value FROM app_settings WHERE key='app_mode'" 2>/dev/null | tr -d ' \r')"
MODE="${MODE:-parser}"

say "Готово ✓"
printf '  Адрес:       http://localhost:%s\n' "$PORT"
printf '  Режим:       %s\n' "$MODE"
printf '  Админ-вход:  %s (заданный пароль)\n' "$SU_USER"
if [ "$MODE" = "parser" ]; then
  cat <<EOF

  Сейчас режим «parser» — только контент и парсер; веб-кабинет и аккаунты
  выключены. Чтобы включить полный сервис (веб-UI, аккаунты, устройства):
  открой админку (/admin), Настройки → «Режим работы» → all, затем перезапусти:
      docker compose up -d
EOF
fi
cat <<EOF

  Логи:        docker compose logs -f app
  Остановить:  docker compose down
EOF
