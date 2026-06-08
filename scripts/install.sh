#!/usr/bin/env bash
# =============================================================================
# movies-go — установка / управление (Docker)
#
#   bash <(curl -fsSL https://raw.githubusercontent.com/Igorek1986/movies-go/main/scripts/install.sh)
#   ./scripts/install.sh                # из клонированного репозитория
#   ./scripts/install.sh [install|update|manage|status|uninstall]
#
# Приложение на Go и работает целиком в Docker — никаких Python/pyenv/Poetry.
# Меню: ↑↓/jk — выбор, 1-9 — быстрый выбор, Enter — подтвердить.
# =============================================================================
set -uo pipefail

REPO="Igorek1986/movies-go"
BRANCH="main"
DUMP_URL="https://github.com/$REPO/releases/latest/download/cards-dump.sql.gz"
APP_CONTAINER="movies-api"      # container_name из docker-compose.yml
DEFAULT_DIR="${MOVIES_GO_DIR:-$HOME/movies-go}"

# ── Цвета / вывод ─────────────────────────────────────────────────────────────
RED=$'\033[0;31m'; GREEN=$'\033[0;32m'; YELLOW=$'\033[1;33m'; BLUE=$'\033[0;34m'; NC=$'\033[0m'
say()  { printf '\n\033[1m%s\033[0m\n' "$*"; }
ok()   { printf '%s✓%s %s\n' "$GREEN" "$NC" "$*"; }
warn() { printf '%s⚠%s %s\n' "$YELLOW" "$NC" "$*"; }
err()  { printf '%s✗%s %s\n' "$RED" "$NC" "$*"; }

# ── Меню со стрелками ─────────────────────────────────────────────────────────
# arrow_menu TITLE item1 item2 ... → индекс выбора в MENU_RESULT
#   ↑↓/jk — перемещение, 1-9 — мгновенный выбор, Home/End/gG — края, Enter — ОК
MENU_RESULT=0
arrow_menu() {
    local title="$1"; shift
    local items=("$@")
    local count=${#items[@]}
    local selected=0
    local total_lines=$(( count + 7 ))
    local first_draw=true

    tput civis 2>/dev/null || true
    trap 'tput cnorm 2>/dev/null || true; exit 130' INT TERM

    while true; do
        if $first_draw; then clear; first_draw=false
        else printf '\033[%dA' "$total_lines"; fi
        printf '\033[J'

        echo ""
        echo "${BLUE}================================================${NC}"
        printf "${BLUE}  %-44s${NC}\n" "$title"
        echo "${BLUE}================================================${NC}"
        echo ""
        local i num
        for i in "${!items[@]}"; do
            num=$(( i + 1 ))
            if [ "$i" -eq "$selected" ]; then
                printf "  ${GREEN}▶ %d) %s${NC}\n" "$num" "${items[$i]}"
            else
                printf "    %d) %s\n" "$num" "${items[$i]}"
            fi
        done
        echo ""
        printf "  ${YELLOW}↑↓/jk — выбор   1-%d — быстро   Enter — ОК${NC}\n" "$count"

        local key
        IFS= read -rsn1 key
        if [[ "$key" == $'\033' ]]; then
            local seq
            IFS= read -rsn2 -t 1 seq || seq=""
            case "$seq" in
                '[A') selected=$(( (selected - 1 + count) % count )) ;;
                '[B') selected=$(( (selected + 1) % count )) ;;
                '[H'|'OH') selected=0 ;;
                '[F'|'OF') selected=$(( count - 1 )) ;;
            esac
        else
            case "$key" in
                k|K) selected=$(( (selected - 1 + count) % count )) ;;
                j|J) selected=$(( (selected + 1) % count )) ;;
                g)   selected=0 ;;
                G)   selected=$(( count - 1 )) ;;
                [1-9])
                    if [ "$key" -le "$count" ]; then
                        tput cnorm 2>/dev/null || true; trap - INT TERM
                        MENU_RESULT=$(( key - 1 )); return 0
                    fi ;;
                "")  tput cnorm 2>/dev/null || true; trap - INT TERM
                     MENU_RESULT=$selected; return 0 ;;
            esac
        fi
    done
}

# ── Docker / Compose ──────────────────────────────────────────────────────────
SUDO=""
dc() { $SUDO docker compose "$@"; }

ensure_docker() {
    if ! command -v docker >/dev/null 2>&1; then
        warn "Docker не установлен."
        printf "  Установить Docker сейчас? [Y/n] "; read -r a </dev/tty
        if [[ ! "${a:-Y}" =~ ^[Nn]$ ]]; then
            say "Установка Docker…"
            curl -fsSL https://get.docker.com -o /tmp/get-docker.sh || { err "Не скачать установщик Docker"; exit 1; }
            sudo sh /tmp/get-docker.sh || { err "Установка Docker не удалась"; exit 1; }
            rm -f /tmp/get-docker.sh
            if [ "$(id -u)" -ne 0 ]; then
                sudo usermod -aG docker "$(id -un)" || true
                warn "Добавил $(id -un) в группу docker — для применения нужен релогин. Пока работаю через sudo."
                SUDO="sudo"
            fi
        else
            err "Docker обязателен."; exit 1
        fi
    fi
    # Нет прав на сокет без sudo? — переключаемся на sudo
    if ! docker info >/dev/null 2>&1; then
        if sudo docker info >/dev/null 2>&1; then SUDO="sudo"
        else err "Docker недоступен (демон не запущен?)."; exit 1; fi
    fi
    dc version >/dev/null 2>&1 || { err "Нужен Docker Compose v2 (docker compose)."; exit 1; }
}

# ── .env helpers ──────────────────────────────────────────────────────────────
# Значение для .env в одинарных кавычках. Для Docker Compose '...' — литерал:
# $ внутри НЕ интерполируется (иначе $ в пароле/токене обрезает значение).
env_q()    { printf "'%s'" "$1"; }
env_val()  { grep -E "^$1=" .env 2>/dev/null | tail -1 | cut -d= -f2- \
             | sed -E "s/^[[:space:]]+//; s/[[:space:]]+$//; s/^'(.*)'$/\1/; s/^\"(.*)\"$/\1/"; }
db_user()  { local v; v="$(env_val DB_USER)"; echo "${v:-movies_api}"; }
db_name()  { local v; v="$(env_val DB_NAME)"; echo "${v:-movies_api}"; }
http_port() { local v; v="$(env_val PORT)"; echo "${v:-8888}"; }

is_installed() {
    [ -f "$ROOT/.env" ] && return 0
    $SUDO docker ps -a --format '{{.Names}}' 2>/dev/null | grep -qx "$APP_CONTAINER"
}

# ── Bootstrap: найти репозиторий или клонировать (для curl-однострочника) ──────
bootstrap_root() {
    local src="${BASH_SOURCE[0]:-}"
    if [ -n "$src" ] && [ -f "$src" ] && [ -f "$(cd "$(dirname "$src")/.." && pwd)/docker-compose.yml" ]; then
        ROOT="$(cd "$(dirname "$src")/.." && pwd)"
    else
        ROOT="$DEFAULT_DIR"
        if [ ! -f "$ROOT/docker-compose.yml" ]; then
            command -v git >/dev/null 2>&1 || { err "Нужен git для клонирования."; exit 1; }
            say "Клонирование $REPO → $ROOT"
            git clone --branch "$BRANCH" "https://github.com/$REPO.git" "$ROOT" \
                || { err "git clone не удался"; exit 1; }
        fi
    fi
    cd "$ROOT" || { err "Не зайти в $ROOT"; exit 1; }
}

# Записать app_mode=all ДО первого старта приложения — тогда оно сразу
# поднимется в режиме all (app_mode читается при запуске), рестарт не нужен.
pre_seed_mode_all() {
    say "Режим all — задаю до первого запуска (рестарт не нужен)…"
    dc up -d db >/dev/null 2>&1 || { warn "БД не поднялась — режим переключишь в /admin."; return; }
    local i
    for i in $(seq 1 30); do
        dc exec -T db pg_isready -U "$(db_user)" -d "$(db_name)" -q 2>/dev/null && break
        sleep 1
    done
    if dc exec -T db psql -U "$(db_user)" "$(db_name)" -q -c \
        "CREATE TABLE IF NOT EXISTS app_settings (key VARCHAR(100) PRIMARY KEY, value TEXT NOT NULL, updated_at TIMESTAMPTZ NOT NULL DEFAULT now());
         INSERT INTO app_settings (key, value) VALUES ('app_mode','all')
         ON CONFLICT (key) DO UPDATE SET value='all';" >/dev/null 2>&1; then
        ok "Режим all включён — сервис стартует сразу в нём"
    else
        warn "Не удалось задать режим — переключишь в /admin → Настройки → Режим работы."
    fi
}

# ── Свежая установка ──────────────────────────────────────────────────────────
do_install() {
    say "Настройка"
    local SU_USER SU_PASS SU_PASS2 TMDB APP_MODE
    read -rp "Логин администратора [admin]: " SU_USER </dev/tty
    SU_USER="${SU_USER:-admin}"
    while :; do
        read -rsp "Пароль администратора: " SU_PASS </dev/tty; echo
        [ -n "$SU_PASS" ] || { warn "Пароль обязателен."; continue; }
        read -rsp "Повторите пароль: " SU_PASS2 </dev/tty; echo
        [ "$SU_PASS" = "$SU_PASS2" ] || { warn "Пароли не совпадают."; continue; }
        break
    done
    echo
    echo "TMDB токен (v4 Read Access Token) — нужен для метаданных. Можно оставить пустым."
    read -rp "TMDB токен: " TMDB </dev/tty
    [ -n "$TMDB" ] && [[ "$TMDB" != Bearer* ]] && TMDB="Bearer $TMDB"

    echo
    echo "Режим работы:"
    echo "  parser — только контент и парсер (по умолчанию)"
    echo "  all    — полный сервис: аккаунты, устройства, веб-кабинет, профили"
    read -rp "Режим [parser/all]: " APP_MODE </dev/tty
    case "${APP_MODE:-parser}" in all|ALL|All) APP_MODE=all ;; *) APP_MODE=parser ;; esac

    umask 077
    # Значения в одинарных кавычках — иначе $ в пароле/токене съедается Docker Compose.
    {
        echo "# Создано scripts/install.sh"
        echo "SUPERUSER_USERNAME=$(env_q "$SU_USER")"
        echo "SUPERUSER_PASSWORD=$(env_q "$SU_PASS")"
        echo "TMDB_TOKEN=$(env_q "$TMDB")"
        echo ""
        echo "# PORT=8888"
        echo "# DB_USER=movies_api"
        echo "# DB_PASSWORD=movies_api"
        echo "# DB_NAME=movies_api"
    } > .env
    ok ".env создан"
    [ -n "$TMDB" ] || warn "TMDB токен пуст — обогащение метаданными выключено (задай TMDB_TOKEN в .env позже)."

    if [ ! -f cards-dump.sql.gz ] && [ ! -f dump.sql.gz ]; then
        say "Залить готовый дамп карточек, чтобы не парсить каталог с нуля?"
        read -rp "Скачать дамп из последнего релиза? [Y/n] " ans </dev/tty
        if [[ ! "${ans:-Y}" =~ ^[Nn]$ ]]; then
            echo "Скачивание дампа…"
            if curl -fL -o cards-dump.sql.gz "$DUMP_URL"; then
                ok "Дамп скачан ($(du -h cards-dump.sql.gz | cut -f1))"
            else
                warn "Не удалось скачать дамп — продолжаю с пустой базой."; rm -f cards-dump.sql.gz
            fi
        fi
    fi

    # Режим all выставляем ДО старта app, чтобы он сразу поднялся в нём (без рестарта).
    [ "$APP_MODE" = "all" ] && pre_seed_mode_all

    if [ -f cards-dump.sql.gz ] || [ -f dump.sql.gz ]; then
        say "Восстановление базы из дампа и запуск сервиса…"
    else
        say "Запуск сервиса…"
    fi
    SUDO="$SUDO" ./scripts/restore.sh || { err "restore.sh завершился с ошибкой"; return 1; }

    print_summary "$SU_USER"
}

print_summary() {
    local su_user="${1:-$(env_val SUPERUSER_USERNAME)}"
    local port mode
    port="$(http_port)"
    mode="$(dc exec -T db psql -U "$(db_user)" "$(db_name)" -tAc \
        "SELECT value FROM app_settings WHERE key='app_mode'" 2>/dev/null | tr -d ' \r')"
    mode="${mode:-parser}"
    say "Готово ✓"
    printf '  Адрес:    http://localhost:%s\n' "$port"
    printf '  Админка:  http://localhost:%s/admin   (логин: %s)\n' "$port" "$su_user"
    printf '  Режим:    %s\n' "$mode"
    if [ "$mode" = "parser" ]; then
        cat <<EOF

  Режим «parser» — только контент и парсер; веб-кабинет и аккаунты выключены.
  Полный сервис: /admin → Настройки → «Режим работы» → all → Сохранить → Управление → Перезапустить.
EOF
    fi
}

# ── Обновление ────────────────────────────────────────────────────────────────
do_update() {
    say "Обновление…"
    if [ -d .git ]; then
        if git diff --quiet && git diff --cached --quiet; then
            git pull --ff-only origin "$BRANCH" || warn "git pull не удался — собираю текущий код"
        else
            warn "Есть локальные изменения — пропускаю git pull, собираю как есть."
        fi
    fi
    dc up -d --build || { err "Сборка/запуск не удались"; return 1; }
    # Пересборка оставляет старый образ как висячий (<none>) — подчищаем,
    # чтобы они не копились на диске. Только висячие, рабочие не трогаем.
    $SUDO docker image prune -f >/dev/null 2>&1 || true
    ok "Обновлено. Адрес: http://localhost:$(http_port)"
}

# ── Управление (Старт / Стоп / Рестарт / Логи) ────────────────────────────────
do_manage() {
    while true; do
        arrow_menu "Управление сервисом" \
            "Запустить        (docker compose up -d)" \
            "Остановить       (docker compose stop)" \
            "Перезапустить    (docker compose restart)" \
            "Логи             (Ctrl-C — назад)" \
            "← Назад"
        case $MENU_RESULT in
            0) say "Запуск…";        dc up -d && ok "Запущено. Адрес: http://localhost:$(http_port)" || err "Не удалось запустить" ;;
            1) say "Остановка…";     dc stop  && ok "Остановлено (данные и контейнеры сохранены)"      || err "Не удалось остановить" ;;
            2) say "Перезапуск…";    dc restart && ok "Перезапущено"                                   || err "Не удалось перезапустить" ;;
            3) say "Логи приложения (Ctrl-C — назад)…"
               trap ' ' INT; dc logs -f --tail=100 app; trap - INT ;;
            4) return ;;
        esac
        [ "$MENU_RESULT" -ne 4 ] && { printf "\n  Enter — продолжить… "; read -r _ </dev/tty; }
    done
}

# ── Статус ────────────────────────────────────────────────────────────────────
do_status() {
    say "Статус movies-go"
    if is_installed; then
        echo ""; dc ps 2>/dev/null || true
        echo ""
        printf '  Режим:  %s\n' "$(dc exec -T db psql -U "$(db_user)" "$(db_name)" -tAc \
            "SELECT value FROM app_settings WHERE key='app_mode'" 2>/dev/null | tr -d ' \r' || echo '?')"
        printf '  Адрес:  http://localhost:%s\n' "$(http_port)"
    else
        warn "Не установлено."
    fi
    if [ -d .git ]; then
        git fetch --quiet origin "$BRANCH" 2>/dev/null || true
        local l r
        l="$(git rev-parse HEAD 2>/dev/null)"; r="$(git rev-parse "origin/$BRANCH" 2>/dev/null)"
        if [ -n "$l" ] && [ -n "$r" ] && [ "$l" != "$r" ]; then
            warn "Доступно обновление (local ${l:0:8} → remote ${r:0:8})."
        else
            ok "Версия актуальна (${l:0:8})."
        fi
    fi
}

# ── Удаление ──────────────────────────────────────────────────────────────────
do_uninstall() {
    say "Удаление"
    printf "  Удалить контейнеры и базу (ВСЕ данные!)? [y/N] "; read -r c </dev/tty
    if [[ ! "${c:-N}" =~ ^[Yy]$ ]]; then echo "Отменено."; return; fi
    dc down -v || warn "docker compose down завершился с ошибкой"
    ok "Контейнеры и БД удалены (.env оставлен)."
    printf "  Удалить также .env (настройки, пароль админа)? [y/N] "; read -r c2 </dev/tty
    [[ "${c2:-N}" =~ ^[Yy]$ ]] && { rm -f .env; ok ".env удалён."; }
}

# ── Главное меню ──────────────────────────────────────────────────────────────
main_menu() {
    while true; do
        if is_installed; then
            arrow_menu "movies-go — установлен" \
                "Обновить" "Управление" "Статус" "Удалить" "Выход"
            case $MENU_RESULT in
                0) do_update ;;
                1) do_manage; continue ;;
                2) do_status ;;
                3) do_uninstall ;;
                4) exit 0 ;;
            esac
        else
            arrow_menu "movies-go — не установлен" "Установить" "Выход"
            case $MENU_RESULT in
                0) do_install ;;
                1) exit 0 ;;
            esac
        fi
        printf "\n  Enter — в меню… "; read -r _ </dev/tty
    done
}

# ── Точка входа ───────────────────────────────────────────────────────────────
bootstrap_root
ensure_docker

case "${1:-}" in
    install)   do_install   ;;
    update)    do_update    ;;
    manage)    do_manage    ;;
    status)    do_status    ;;
    uninstall) do_uninstall ;;
    "")        main_menu    ;;
    *) echo "Usage: $0 [install|update|manage|status|uninstall]"; exit 1 ;;
esac
