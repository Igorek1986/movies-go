(function () {
    'use strict';

    var VERSION = '1.0.0';

    var DEBUG = false;
    function log(message, data) {
        if (DEBUG) console.log('[NPUnwatched] ' + message, data !== undefined ? data : '');
    }

    // =========================================================================
    // Стили
    // =========================================================================

    var style = document.createElement('style');
    style.textContent = [
        '.np-unwatched-remaining {',
        '    position: absolute; right: 0; top: 0;',
        '    padding: 0.2em 0.5em; font-size: 1.1em; border-radius: 0 1em;',
        '    font-weight: bold; z-index: 2;',
        '    background: rgba(76,175,80,0.85); color: #fff;',
        '    transition: all 0.3s ease;',
        '}',
        // status.js (вариант 2) занимает правый верхний угол статусом сериала —
        // счётчик остатка сдвигаем ниже него.
        'body[data-status-badge-style="2"] .card .view--has-status .np-unwatched-remaining {',
        '    top: 1.6em;',
        '}',
        '.np-unwatched-progress {',
        '    position: absolute; left: 0; bottom: 0;',
        '    padding: 0.2em 0.5em; font-size: 1em; border-radius: 0.5em 0;',
        '    font-weight: bold; z-index: 2;',
        '    background: rgba(0,0,0,0.6); color: #fff;',
        '    transition: all 0.3s ease;',
        '}',
        '.np-unwatched-next {',
        '    position: absolute; left: 0; bottom: 1.6em;',
        '    padding: 0.2em 0.5em; font-size: 1em; border-radius: 0.5em 0;',
        '    font-weight: bold; z-index: 2;',
        '    background: rgba(33,150,243,0.85); color: #fff;',
        '    transition: all 0.3s ease;',
        '}',
        '.full-start-new__poster { position: relative; }',
        '.full-start-new__poster .np-unwatched-remaining,',
        '.full-start-new__poster .np-unwatched-progress,',
        '.full-start-new__poster .np-unwatched-next {',
        '    font-size: 1.2em;',
        '}',
        '.full-start-new__poster .np-unwatched-progress { bottom: 0.5em; left: 0.5em; }',
        '.full-start-new__poster .np-unwatched-next     { bottom: 2em;   left: 0.5em; }',
        '.full-start-new__poster .np-unwatched-remaining { right: 0.5em; top: 0.5em; }',
        'body.true--mobile.orientation--portrait .full-start-new__poster .np-unwatched-progress  { bottom: 15em; }',
        'body.true--mobile.orientation--portrait .full-start-new__poster .np-unwatched-next       { bottom: 17em; }',
        'body.true--mobile.orientation--landscape .full-start-new__poster .np-unwatched-progress  { bottom: 2.5em; }',
        'body.true--mobile.orientation--landscape .full-start-new__poster .np-unwatched-next       { bottom: 4em; }',
        '@keyframes npUnwatchedFlip {',
        '    0%   { transform: scale(1); }',
        '    50%  { transform: scale(1.12); }',
        '    100% { transform: scale(1); }',
        '}',
        '.np-unwatched-flip { animation: npUnwatchedFlip 0.4s ease; }',
    ].join('\n');
    document.head.appendChild(style);

    // =========================================================================
    // Конфигурация сервера (общая с np.js)
    // =========================================================================

    function getNpToken() { return Lampa.Storage.get('numparser_api_key', ''); }
    function getNpBaseUrl() { return Lampa.Storage.get('base_url_numparser', ''); }

    // =========================================================================
    // Профильные настройки (канонический рецепт — см. plugins/status.js)
    // =========================================================================

    function getProfileId() {
        if (window._np_profiles_started || window.profiles_plugin) {
            var lampacId = Lampa.Storage.get('lampac_profile_id', '');
            if (lampacId) return String(lampacId);
        }
        try {
            if (Lampa.Account.Permit.account && Lampa.Account.Permit.account.profile &&
                Lampa.Account.Permit.account.profile.id) {
                return String(Lampa.Account.Permit.account.profile.id);
            }
        } catch (e) {}
        return '';
    }

    function getProfileKey(baseKey) {
        var profileId = getProfileId();
        if (profileId && profileId.charAt(0) === '_') profileId = profileId.slice(1);
        return profileId ? baseKey + '_profile_' + profileId : baseKey;
    }

    function getProfileSetting(key, defaultValue) {
        return Lampa.Storage.get(getProfileKey(key), defaultValue);
    }

    function isTrue(v) { return v === true || v === 'true'; }

    var SETTINGS_COMPONENT = 'np_unwatched_settings';
    var ENABLED_KEY   = 'np_unwatched_enabled';
    var PROGRESS_KEY  = 'np_unwatched_badge_progress';
    var REMAINING_KEY = 'np_unwatched_badge_remaining';
    var NEXT_KEY      = 'np_unwatched_badge_next';
    var MIN_PROGRESS_KEY = 'np_unwatched_min_progress';
    var DEFAULT_MIN_PROGRESS = '90';
    var SYNC_PLUGIN = 'np_unwatched';
    var SYNC_KEYS   = [ENABLED_KEY, PROGRESS_KEY, REMAINING_KEY, NEXT_KEY, MIN_PROGRESS_KEY];

    // Булевы в Storage пишем строками 'true'/'false' — Storage.get затирает
    // закешированный boolean false дефолтом (value || empty), строка выживает.
    function storableValue(v) {
        if (v === true) return 'true';
        if (v === false) return 'false';
        return v;
    }

    var _syncApplying = false;

    function setProfileSetting(key, value, sync) {
        value = storableValue(value);
        Lampa.Storage.set(getProfileKey(key), value);
        if (sync !== false && !_syncApplying && window.__NMSync) {
            window.__NMSync.patch(SYNC_PLUGIN, getProfileKey(key), value);
        }
    }

    function hasProfileSetting(key) {
        return window.localStorage.getItem(getProfileKey(key)) !== null;
    }

    function _applySetting(profileKey, value) {
        if (profileKey.indexOf('_profile_') < 0) return;
        value = storableValue(value);
        _syncApplying = true;
        Lampa.Storage.set(profileKey, value);
        _syncApplying = false;
    }

    function registerNMSync() {
        if (!window.__NMSync) return;
        window.__NMSync.register(SYNC_PLUGIN, [], _applySetting, function (serverKeys) {
            SYNC_KEYS.forEach(function (key) {
                var profileKey = getProfileKey(key);
                if (serverKeys.indexOf(profileKey) < 0 && hasProfileSetting(key)) {
                    setProfileSetting(key, getProfileSetting(key));
                }
            });
        });
    }

    function loadProfileSettings() {
        if (!hasProfileSetting(ENABLED_KEY)) setProfileSetting(ENABLED_KEY, true, false);
        if (!hasProfileSetting(PROGRESS_KEY)) setProfileSetting(PROGRESS_KEY, true, false);
        if (!hasProfileSetting(REMAINING_KEY)) setProfileSetting(REMAINING_KEY, true, false);
        if (!hasProfileSetting(NEXT_KEY)) setProfileSetting(NEXT_KEY, true, false);
        if (!hasProfileSetting(MIN_PROGRESS_KEY)) setProfileSetting(MIN_PROGRESS_KEY, DEFAULT_MIN_PROGRESS, false);

        Lampa.Storage.set(ENABLED_KEY, storableValue(getProfileSetting(ENABLED_KEY, true)), true);
        Lampa.Storage.set(PROGRESS_KEY, storableValue(getProfileSetting(PROGRESS_KEY, true)), true);
        Lampa.Storage.set(REMAINING_KEY, storableValue(getProfileSetting(REMAINING_KEY, true)), true);
        Lampa.Storage.set(NEXT_KEY, storableValue(getProfileSetting(NEXT_KEY, true)), true);
        Lampa.Storage.set(MIN_PROGRESS_KEY, getProfileSetting(MIN_PROGRESS_KEY, DEFAULT_MIN_PROGRESS), true);
    }

    function isPluginEnabled() {
        return isTrue(getProfileSetting(ENABLED_KEY, true));
    }

    function initSettings() {
        if (!Lampa.SettingsApi) return;

        Lampa.SettingsApi.addComponent({
            component: SETTINGS_COMPONENT,
            name: 'Непросмотренные',
            icon: '<svg width="24" height="24" viewBox="0 0 24 24"><circle cx="12" cy="12" r="10" fill="#4CAF50"/><path d="M8 12l2.5 2.5L16 9" stroke="#fff" stroke-width="2" fill="none"/></svg>',
        });

        Lampa.SettingsApi.addParam({
            component: SETTINGS_COMPONENT,
            param: { name: ENABLED_KEY, type: 'trigger', default: true },
            field: {
                name: 'Показывать бейджи прогресса',
                description: 'Метки на карточках сериалов из категории «Непросмотренные»: сколько серий осталось, прогресс, следующая серия.',
            },
            onChange: function (value) { setProfileSetting(ENABLED_KEY, value === true || value === 'true'); },
        });

        Lampa.SettingsApi.addParam({
            component: SETTINGS_COMPONENT,
            param: {
                name: MIN_PROGRESS_KEY, type: 'select',
                values: { '70': '70%', '80': '80%', '90': '90%', '95': '95%', '100': '100%' },
                default: DEFAULT_MIN_PROGRESS,
            },
            field: {
                name: 'Порог просмотра серии',
                description: 'С какого процента просмотра серия считается просмотренной.',
            },
            onChange: function (value) { setProfileSetting(MIN_PROGRESS_KEY, value.toString()); },
        });

        Lampa.SettingsApi.addParam({
            component: SETTINGS_COMPONENT,
            param: { name: PROGRESS_KEY, type: 'trigger', default: true },
            field: { name: 'Метка прогресса', description: 'Просмотрено/вышло, например 5/12' },
            onChange: function (value) { setProfileSetting(PROGRESS_KEY, value === true || value === 'true'); },
        });

        Lampa.SettingsApi.addParam({
            component: SETTINGS_COMPONENT,
            param: { name: REMAINING_KEY, type: 'trigger', default: true },
            field: { name: 'Счётчик оставшихся серий' },
            onChange: function (value) { setProfileSetting(REMAINING_KEY, value === true || value === 'true'); },
        });

        Lampa.SettingsApi.addParam({
            component: SETTINGS_COMPONENT,
            param: { name: NEXT_KEY, type: 'trigger', default: true },
            field: { name: 'Метка следующей серии', description: 'Например S01E05' },
            onChange: function (value) { setProfileSetting(NEXT_KEY, value === true || value === 'true'); },
        });
    }

    // =========================================================================
    // Сеть
    // =========================================================================

    function isTvShow(card) {
        if (!card) return false;
        return !!(card.number_of_seasons || card.seasons || card.first_air_date || card.original_name);
    }

    function cardIdOf(card) {
        if (!card || !card.id) return '';
        return card.id + '_tv';
    }

    // Точечный запрос прогресса ОДНОГО сериала — не тянем весь список
    // «Непросмотренные», только карточку, которую сейчас смотрим/открыли.
    function fetchProgress(cardId, callback) {
        var token = getNpToken();
        var base = getNpBaseUrl();
        if (!token || !base || !cardId) { callback(null); return; }

        var minProgress = getProfileSetting(MIN_PROGRESS_KEY, DEFAULT_MIN_PROGRESS);
        var url = base + '/unwatched/progress?token=' + encodeURIComponent(token) +
            '&card_id=' + encodeURIComponent(cardId) +
            '&percent=' + encodeURIComponent(minProgress);
        var profileId = getProfileId();
        if (profileId) url += '&profile_id=' + encodeURIComponent(profileId);

        var network = new Lampa.Reguest();
        network.timeout(8000);
        network.silent(url, function (json) {
            callback(json && json.found ? json : null);
        }, function (err) {
            log('fetchProgress error', err);
            callback(null);
        });
    }

    function padTwo(n) { n = parseInt(n, 10) || 0; return n < 10 ? '0' + n : '' + n; }
    function nextEpisodeText(ep) {
        if (!ep) return '';
        return 'S' + padTwo(ep.season_number) + 'E' + padTwo(ep.episode_number);
    }

    // =========================================================================
    // Карточки в строках (данные уже приезжают в самой карточке из /unwatched)
    // =========================================================================

    var processedRowCards = [];

    function addBadgesToRowCard(cardHtml) {
        if (!isPluginEnabled()) return;

        var cardElement = cardHtml && cardHtml.get ? cardHtml.get(0) : (cardHtml && cardHtml[0] ? cardHtml[0] : cardHtml);
        if (!cardElement) return;
        if (processedRowCards.indexOf(cardElement) !== -1) return;

        var data = cardElement.card_data || cardElement.data || {};
        // Бейджи есть только у карточек, отданных категорией /unwatched —
        // у остальных этих полей просто нет, ничего не рисуем.
        if (data.unwatched_count === undefined || data.unwatched_count === null) return;

        var cardView = cardElement.querySelector('.card__view');
        if (!cardView) return;

        processedRowCards.push(cardElement);
        renderBadges(cardView, data);
    }

    function renderBadges(container, data) {
        var old = container.querySelectorAll('.np-unwatched-progress, .np-unwatched-remaining, .np-unwatched-next');
        for (var i = 0; i < old.length; i++) old[i].remove();

        if (isTrue(getProfileSetting(REMAINING_KEY, true)) && data.unwatched_count) {
            var r = document.createElement('div');
            r.className = 'np-unwatched-remaining';
            r.textContent = data.unwatched_count;
            container.appendChild(r);
        }
        if (isTrue(getProfileSetting(PROGRESS_KEY, true)) && data.aired_count) {
            var p = document.createElement('div');
            p.className = 'np-unwatched-progress';
            p.textContent = (data.watched_count || 0) + '/' + data.aired_count;
            container.appendChild(p);
        }
        if (isTrue(getProfileSetting(NEXT_KEY, true)) && data.next_episode) {
            var n = document.createElement('div');
            n.className = 'np-unwatched-next';
            n.textContent = nextEpisodeText(data.next_episode);
            container.appendChild(n);
        }
    }

    function removeBadges(container) {
        var old = container.querySelectorAll('.np-unwatched-progress, .np-unwatched-remaining, .np-unwatched-next');
        for (var i = 0; i < old.length; i++) {
            (function (el) {
                el.style.transition = 'opacity 0.4s ease, transform 0.4s ease';
                el.style.opacity = '0';
                el.style.transform = 'translateY(10px)';
                setTimeout(function () { if (el.parentNode) el.remove(); }, 400);
            })(old[i]);
        }
    }

    // =========================================================================
    // Полная карточка (full/detail)
    // =========================================================================

    function isSameFullCardOpen(card) {
        if (!card || !card.id) return true;
        var active = Lampa.Activity.active && Lampa.Activity.active();
        if (!active || active.component !== 'full') return false;
        var openCard = active.card_data || active.card || active.movie;
        if (!openCard || !openCard.id) return true;
        return String(openCard.id) === String(card.id);
    }

    function renderFullCardBadges(posterEl, progress) {
        var old = posterEl.querySelectorAll('.np-unwatched-progress, .np-unwatched-remaining, .np-unwatched-next');
        for (var i = 0; i < old.length; i++) old[i].remove();
        renderBadges(posterEl, {
            unwatched_count: progress.unwatched_count,
            watched_count: progress.watched_count,
            aired_count: progress.aired_count,
            next_episode: progress.next_episode,
        });
    }

    Lampa.Listener.follow('full', function (event) {
        if (event.type !== 'complite' || !event.data || !event.data.movie) return;
        var movie = event.data.movie;
        if (!isPluginEnabled() || !isTvShow(movie)) return;

        fetchProgress(cardIdOf(movie), function (progress) {
            if (!isSameFullCardOpen(movie)) return; // ушли на другую карточку, пока грузилось
            var posterEl = event.body ? event.body.find('.full-start-new__poster') : $('.full-start-new__poster');
            if (!posterEl || !posterEl.length) return;
            if (progress) renderFullCardBadges(posterEl[0], progress);
            else removeBadges(posterEl[0]);
        });
    });

    // =========================================================================
    // Живое обновление при просмотре (Lampa.Timeline)
    // =========================================================================

    function getCurrentCard() {
        var active = Lampa.Activity.active && Lampa.Activity.active();
        return (active && (active.card_data || active.card || active.movie)) || null;
    }

    var lastProcessedKey = ''; // защита от повторной обработки одного и того же события

    function processTimelineUpdate(e) {
        if (!isPluginEnabled()) return;
        if (!e || !e.data || !e.data.hash || !e.data.road) return;

        var percent = e.data.road.percent;
        var minProgress = parseInt(getProfileSetting(MIN_PROGRESS_KEY, DEFAULT_MIN_PROGRESS), 10);
        if (percent < minProgress) return;

        var card = getCurrentCard();
        if (!card || !isTvShow(card)) return;

        var cardId = cardIdOf(card);
        var key = cardId + ':' + e.data.hash;
        if (key === lastProcessedKey) return; // тот же тик Timeline — не дёргаем сервер повторно
        lastProcessedKey = key;

        // Небольшая задержка — даём серверу время сохранить сам таймкод
        // (событие Timeline может обогнать запись в БД на десятки мс).
        setTimeout(function () {
            fetchProgress(cardId, function (progress) {
                if (progress) updateBadgesEverywhere(cardId, progress);
            });
        }, 400);
    }

    function updateBadgesEverywhere(cardId, progress) {
        // Полная карточка, если открыта именно эта
        var active = Lampa.Activity.active && Lampa.Activity.active();
        if (active && active.component === 'full') {
            var openCard = active.card_data || active.card || active.movie;
            if (openCard && cardIdOf(openCard) === cardId) {
                var posterEl = document.querySelector('.full-start-new__poster');
                if (posterEl) animateBadgeUpdate(posterEl, progress);
            }
        }

        // Карточки в строках (каталог/подборки/непросмотренные), видимые в DOM
        var cards = document.querySelectorAll('.card');
        for (var i = 0; i < cards.length; i++) {
            var cardElement = cards[i];
            var data = cardElement.card_data || cardElement.data;
            if (!data || cardIdOf(data) !== cardId) continue;

            data.unwatched_count = progress.unwatched_count;
            data.watched_count = progress.watched_count;
            data.aired_count = progress.aired_count;
            data.next_episode = progress.next_episode;

            var cardView = cardElement.querySelector('.card__view');
            if (cardView) animateBadgeUpdate(cardView, progress);
        }
    }

    function animateBadgeUpdate(container, progress) {
        var remainingEl = container.querySelector('.np-unwatched-remaining');
        var progressEl = container.querySelector('.np-unwatched-progress');
        var nextEl = container.querySelector('.np-unwatched-next');

        if (progress.unwatched_count <= 0) {
            // Досмотрели всё, что было вышедшим — снимаем бейджи.
            removeBadges(container);
            return;
        }

        if (isTrue(getProfileSetting(REMAINING_KEY, true))) {
            if (remainingEl) animateCounter(remainingEl, parseInt(remainingEl.textContent, 10) || 0, progress.unwatched_count);
            else { var r = document.createElement('div'); r.className = 'np-unwatched-remaining'; r.textContent = progress.unwatched_count; container.appendChild(r); }
        }
        if (isTrue(getProfileSetting(PROGRESS_KEY, true)) && progress.aired_count) {
            var newProgressText = (progress.watched_count || 0) + '/' + progress.aired_count;
            if (progressEl) animateDigitByDigit(progressEl, progressEl.textContent, newProgressText, progress.aired_count);
            else { var p = document.createElement('div'); p.className = 'np-unwatched-progress'; p.textContent = newProgressText; container.appendChild(p); }
        }
        if (isTrue(getProfileSetting(NEXT_KEY, true)) && progress.next_episode) {
            var newNextText = nextEpisodeText(progress.next_episode);
            if (nextEl) animateNextEpisode(nextEl, nextEl.textContent, newNextText);
            else { var n = document.createElement('div'); n.className = 'np-unwatched-next'; n.textContent = newNextText; container.appendChild(n); }
        }
    }

    // =========================================================================
    // Анимации (портировано из myshows.js, самодостаточно — не зависит от MyShows)
    // =========================================================================

    function flash(el) {
        el.classList.remove('np-unwatched-flip');
        void el.offsetWidth; // reflow — перезапустить анимацию, если класс уже был
        el.classList.add('np-unwatched-flip');
        setTimeout(function () { el.classList.remove('np-unwatched-flip'); }, 400);
    }

    // Счётчик остатка серий: считаем от старого числа к новому по шагам.
    function animateCounter(container, startNum, endNum) {
        if (startNum === endNum) { flash(container); return; }
        var direction = startNum < endNum ? 'up' : 'down';
        var current = startNum;
        var speed = 200;

        function step() {
            container.textContent = current;
            setTimeout(function () {
                if (direction === 'up' && current < endNum) { current++; setTimeout(step, speed); }
                else if (direction === 'down' && current > endNum) { current--; setTimeout(step, speed); }
            }, 60);
        }
        step();
    }

    // Прогресс "X/Y" — считаем первую цифру, вторая (общее число серий) не меняется
    // в рамках одного показа (меняется только когда выходит новая серия — это уже
    // покроет следующий live-апдейт).
    function animateDigitByDigit(container, oldText, newText, totalEpisodes) {
        var oldParts = (oldText || '').split('/');
        var newParts = (newText || '').split('/');
        if (oldParts.length !== 2 || newParts.length !== 2) { container.textContent = newText; flash(container); return; }

        var oldWatched = parseInt(oldParts[0], 10);
        var newWatched = parseInt(newParts[0], 10);
        if (isNaN(oldWatched) || isNaN(newWatched) || oldWatched === newWatched) {
            container.textContent = newText;
            flash(container);
            return;
        }

        var direction = oldWatched < newWatched ? 'up' : 'down';
        var current = oldWatched;
        var speed = 200;

        function step() {
            container.textContent = current + '/' + totalEpisodes;
            setTimeout(function () {
                if (direction === 'up' && current < newWatched) { current++; setTimeout(step, speed); }
                else if (direction === 'down' && current > newWatched) { current--; setTimeout(step, speed); }
            }, 60);
        }
        step();
    }

    // Следующая серия "S01E05" — считаем по эпизоду в рамках сезона; при смене
    // сезона просто переключаем с анимацией-вспышкой (редкое событие).
    function animateNextEpisode(container, oldText, newText) {
        oldText = (oldText || '').trim();
        newText = (newText || '').trim();
        if (oldText === newText) return;

        var oldMatch = oldText.match(/^S(\d+)E(\d+)$/);
        var newMatch = newText.match(/^S(\d+)E(\d+)$/);
        if (!oldMatch || !newMatch) { container.textContent = newText; flash(container); return; }

        var oldSeason = parseInt(oldMatch[1], 10), oldEp = parseInt(oldMatch[2], 10);
        var newSeason = parseInt(newMatch[1], 10), newEp = parseInt(newMatch[2], 10);

        if (oldSeason !== newSeason) {
            container.textContent = newText;
            flash(container);
            return;
        }

        var direction = oldEp < newEp ? 'up' : 'down';
        var current = oldEp;
        var speed = 200;

        function step() {
            container.textContent = 'S' + padTwo(oldSeason) + 'E' + padTwo(current);
            setTimeout(function () {
                if (direction === 'up' && current < newEp) { current++; setTimeout(step, speed); }
                else if (direction === 'down' && current > newEp) { current--; setTimeout(step, speed); }
            }, 60);
        }
        step();
    }

    // =========================================================================
    // Инициализация
    // =========================================================================

    function onProfileChanged() {
        loadProfileSettings();
        processedRowCards = [];
    }

    function init() {
        var isLampa3 = Lampa.Manifest && Lampa.Manifest.app_digital >= 300;

        loadProfileSettings();
        initSettings();
        registerNMSync();

        Lampa.Listener.follow('profile', function (e) { if (e.type === 'changed') onProfileChanged(); });
        if (Lampa.Account && Lampa.Account.listener) {
            Lampa.Account.listener.follow('profile_select', function () { onProfileChanged(); });
        }
        Lampa.Listener.follow('profile_select', function () { onProfileChanged(); });
        Lampa.Listener.follow('state:changed', function (e) {
            if (e.target === 'favorite' && e.reason === 'profile') onProfileChanged();
        });

        if (window.Lampa && Lampa.Timeline && Lampa.Timeline.listener) {
            Lampa.Timeline.listener.follow('update', processTimelineUpdate);
        }

        if (isLampa3 && Lampa.Maker && Lampa.Maker.map) {
            try {
                var cardMap = Lampa.Maker.map('Card');
                if (cardMap && cardMap.Card && cardMap.Card.onVisible) {
                    var originalOnVisible = cardMap.Card.onVisible;
                    cardMap.Card.onVisible = function () {
                        originalOnVisible.call(this);
                        if (isPluginEnabled() && this.html) addBadgesToRowCard(this.html);
                    };
                }
            } catch (e) {
                log('Card.onVisible intercept failed: ' + e);
            }
        }

        log('Initialization complete, profile: ' + getProfileId());
    }

    function boot() {
        init();
        try {
            Lampa.Manifest.plugins = {
                type: 'other',
                version: VERSION,
                name: 'NP Unwatched',
                description: 'Бейджи прогресса просмотра на карточках «Непросмотренные» (локальные данные, без MyShows)'
            };
        } catch (e) {}
        console.log('NPUnwatched', 'plugin ready, version', VERSION);
    }

    if (window.appready) {
        boot();
    } else {
        Lampa.Listener.follow('app', function (e) { if (e.type === 'ready') boot(); });
    }
})();
