# Ветки, релизы и публикация дампов

## Ветки

| Ветка | Назначение |
|-------|-----------|
| `dev`  | Активная разработка. Сюда коммитим каждый день. |
| `main` | Стабильная. Сюда попадает только то, что готово к релизу. На коммитах `main` стоят теги версий. |

Правило: **в `main` никогда не коммитим напрямую** — только мёржим из `dev`.

---

## Ежедневная разработка (на `dev`)

```bash
git checkout dev
# ... работа ...
git add -A
git commit -m "feat: что сделал"      # формат Conventional Commits (feat/fix/chore/...)
git push origin dev
```

Коммиты в формате Conventional Commits (`feat:`, `fix:`, `chore:`, `refactor:`, `docs:`) —
из них автоматически собирается changelog в Release.

---

## Выпуск релиза (`dev` → `main` → тег)

Когда в `dev` накопилось готовое и хочется выпустить версию:

```bash
# 1. Убедись, что dev запушен и чистый
git checkout dev
git status                 # должно быть "nothing to commit"
git push origin dev

# 2. Переключись на main и подтяни dev
git checkout main
git pull origin main
git merge --ff-only dev    # быстрый перемот; если не получается — git merge --no-ff dev
git push origin main

# 3. Поставь тег и выпусти релиз одним скриптом
./scripts/release.sh 1.1.0     # без префикса v — скрипт сам добавит

# 4. Вернись в dev и работай дальше
git checkout dev
```

`scripts/release.sh`:
1. проверяет, что ты на `main` и дерево чистое;
2. ставит тег `vX.Y.Z` и пушит его → триггерит сборку Docker-образа в GHCR;
3. делает **публично-безопасный** дамп карточек (`dump-cards.sh`);
4. создаёт GitHub Release с авто-changelog и прикладывает `cards-dump.sql.gz`.

### Что значит номер версии (SemVer)

`MAJOR.MINOR.PATCH`, например `1.4.2`:
- **PATCH** (`1.4.2`) — багфиксы, ничего не сломано.
- **MINOR** (`1.5.0`) — новые фичи, обратная совместимость сохранена.
- **MAJOR** (`2.0.0`) — ломающие изменения.

---

## Публикация дампа карточек

⚠️ **Важно про безопасность.** Есть два дампа:

| Скрипт | Содержит | Куда |
|--------|----------|------|
| `scripts/dump.sh` | `media_cards`, `torrents`, `episodes` **+ `app_settings`** | **только локально** — `app_settings` содержит секреты (telegram_bot_token, kinozal_login/password, аналитику). **Публиковать нельзя.** |
| `scripts/dump-cards.sh` | только `media_cards`, `torrents`, `episodes` | **публично-безопасно** — это и идёт в Release. |

Пользовательских данных (`users`, `devices`/токены, `timecodes`, `myshows_*`, `telegram_*`)
нет ни в одном из дампов.

### Разовая публикация
Делается автоматически при `./scripts/release.sh` (дамп прикладывается к версии).

### Ежедневный авто-дамп (cron на сервере)

Облачный CI не видит твою БД, поэтому крутим на сервере, где живёт докер:

```bash
# crontab -e — каждый день в 04:00
0 4 * * * cd /path/to/movies-go && ./scripts/publish-cards-dump.sh >> /var/log/cards-dump.log 2>&1
```

`scripts/publish-cards-dump.sh` обновляет один постоянный релиз с тегом `cards-db`
(ассет перезаписывается, новые теги не плодятся).

---

## Первичная настройка (один раз)

```bash
# remote
git remote add origin git@github.com:Igorek1986/movies-go.git

# gh CLI (нужен для создания релизов и заливки дампов)
brew install gh
gh auth login
```

После пуша тега в GitHub во вкладке **Actions** соберётся образ в GHCR
(`ghcr.io/igorek1986/movies-go:1.0.0` и `:latest`).
