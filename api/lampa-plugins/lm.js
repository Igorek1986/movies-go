(function () {
    'use strict';

    var DEFAULT_SOURCE_NAME = 'MediaHub';
    var SOURCE_NAME = Lampa.Storage.get('mediahub_source_name', DEFAULT_SOURCE_NAME);
    var newName = SOURCE_NAME;
    var BASE_URL = (function () {
        var src = (document.currentScript && document.currentScript.src) || '';
        var params = new URLSearchParams(src.split('?')[1] || '');
        var explicit = (params.get('base_url') || '').replace(/\/$/, '');
        if (explicit) return explicit;
        // Fallback: use origin of the script itself
        try {
            var u = new URL(src);
            return u.origin;
        } catch (e) { return ''; }
    })();
    var ICON = '<svg version="1.1" xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" x="0px" y="0px" viewBox="0 0 512 512" style="enable-background:new 0 0 512 512;" xml:space="preserve"><g><g><path fill="currentColor" d="M482.909,67.2H29.091C13.05,67.2,0,80.25,0,96.291v319.418C0,431.75,13.05,444.8,29.091,444.8h453.818c16.041,0,29.091-13.05,29.091-29.091V96.291C512,80.25,498.95,67.2,482.909,67.2z M477.091,409.891H34.909V102.109h442.182V409.891z"/></g></g><g><g><rect fill="currentColor" x="126.836" y="84.655" width="34.909" height="342.109"/></g></g><g><g><rect fill="currentColor" x="350.255" y="84.655" width="34.909" height="342.109"/></g></g><g><g><rect fill="currentColor" x="367.709" y="184.145" width="126.836" height="34.909"/></g></g><g><g><rect fill="currentColor" x="17.455" y="184.145" width="126.836" height="34.909"/></g></g><g><g><rect fill="currentColor" x="367.709" y="292.364" width="126.836" height="34.909"/></g></g><g><g><rect fill="currentColor" x="17.455" y="292.364" width="126.836" height="34.909"/></g></g></svg>';
    var DEFAULT_MIN_PROGRESS = 90;
    var MIN_PROGRESS = Lampa.Storage.get('mediahub_min_progress', DEFAULT_MIN_PROGRESS);
    var newProgress = MIN_PROGRESS;
    var MEDIAHUB_HIDE_WATCHED = null;

    Lampa.Storage.set('base_url_mediahub', BASE_URL);

    // ── Debug logging ─────────────────────────────────────────────────────────────

    function createLog(consoleMethod) {
        var DEBUG = Lampa.Storage.get('mediahub_debug_mode', false);
        if (!DEBUG) return function () {};
        return function () {
            var a = Array.prototype.slice.call(arguments);
            a.unshift('MediaHub');
            consoleMethod.apply(console, a);
        };
    }
    var Log = {
        info:  createLog(console.log),
        error: createLog(console.error),
        warn:  createLog(console.warn)
    };

    // ── Categories ────────────────────────────────────────────────────────────────

    function getAllCategories() {
        var currentYear = new Date().getFullYear();
        var list = [
            { key: 'lm_np_popular',      title: 'Популярное' },
            { key: 'myshows_unwatched',  title: 'Непросмотренные (MyShows)' },
            { key: 'lm_movies_ru_new',   title: 'Новые русские фильмы' },
            { key: 'lm_movies_new',      title: 'Новые фильмы' },
            { key: 'lm_tv_shows',        title: 'Сериалы' },
            { key: 'lm_tv_shows_ru',     title: 'Русские сериалы' },
            { key: 'continues',          title: 'Продолжить просмотр MediaHub' },
            { key: 'continues_movie',    title: 'Продолжить просмотр (Фильмы)' },
            { key: 'continues_tv',       title: 'Продолжить просмотр (Сериалы)' },
            { key: 'continues_anime',    title: 'Продолжить просмотр (Аниме)' },
            { key: 'episodes',           title: 'Ближайшие выходы эпизодов' },
            { key: 'recent',             title: 'Недавние выходы эпизодов' },
            { key: 'lm_movies_4k_new',  title: 'В высоком качестве (новые)' },
            { key: 'lm_legends_id',     title: 'Топ фильмы' },
            { key: 'lm_movies_4k',      title: 'В высоком качестве' },
            { key: 'lm_movies',         title: 'Фильмы' },
            { key: 'lm_movies_ru',      title: 'Русские фильмы' },
            { key: 'lm_cartoon_movies', title: 'Мультфильмы' },
            { key: 'lm_cartoon_series', title: 'Мультсериалы' },
            { key: 'lm_anime',          title: 'Аниме' }
        ];
        for (var y = currentYear; y >= 1980; y--) {
            list.push({ key: 'year_' + y, title: 'Фильмы ' + y + ' года' });
        }
        return list;
    }

    // ── API service ───────────────────────────────────────────────────────────────

    function MediaHubApiService() {
        var self = this;
        self.network = new Lampa.Reguest();
        self.discovery = false;

        // Maps a category key to our API path. Returns '' for Lampa built-ins.
        function keyToPath(key) {
            if (key.indexOf('lm_') === 0) return key.slice(3);           // lm_movies_new → movies_new
            if (key.indexOf('year_') === 0) return 'movies_id_' + key.slice(5); // year_2024 → movies_id_2024
            if (key === 'continues' || key.indexOf('continues_') === 0) return key; // continues → /continues
            return '';  // Lampa built-in
        }

        function buildUrl(key, page) {
            var path = keyToPath(key);
            if (!path || !BASE_URL) return '';

            var url = BASE_URL + '/' + path + '?page=' + (page || 1);

            var token = Lampa.Storage.get('mediahub_api_key', '');
            if (token) {
                url += '&token=' + encodeURIComponent(token);

                var hideWatched = getProfileSetting('mediahub_hide_watched', 'true');
                if (hideWatched === true || hideWatched === 'true') {
                    url += '&hide_watched=1';
                    var pct = getProfileSetting('mediahub_min_progress', DEFAULT_MIN_PROGRESS);
                    url += '&percent=' + encodeURIComponent(pct);
                }
                var profileId = getProfileId();
                if (profileId) url += '&profile_id=' + encodeURIComponent(profileId);
            }

            return url;
        }

        function normalizeData(json, callback) {
            var qualityMode = getProfileSetting('mediahub_quality_mode', 'simple');
            var sourceName  = getProfileSetting('mediahub_source_name', DEFAULT_SOURCE_NAME);

            var normalized = {
                results: (json.results || []).map(function (item) {
                    var poster_path = item.poster_path || item.poster || '';
                    if (poster_path && poster_path.indexOf('http') === 0) {
                        var match = poster_path.match(/\/t\/p\/[^\/]+\/(.+)$/);
                        poster_path = match ? '/' + match[1] : '';
                    }

                    var dataItem = {
                        id:                item.id,
                        poster_path:       poster_path,
                        img:               item.img,
                        overview:          item.overview || item.description || '',
                        vote_average:      item.vote_average || 0,
                        backdrop_path:     item.backdrop_path || item.backdrop || '',
                        background_image:  item.background_image,
                        source:            sourceName,
                        media_type:        item.media_type || ((item.first_air_date || item.number_of_seasons) ? 'tv' : 'movie'),
                        original_title:    item.original_title || item.original_name || '',
                        title:             item.title || item.name || '',
                        original_language: item.original_language || 'ru',
                        first_air_date:    item.first_air_date,
                        number_of_seasons: item.number_of_seasons,
                        status:            item.status || ''
                    };

                    if (item.release_quality) {
                        dataItem.release_quality = qualityMode === 'simple'
                            ? getQuality(item.release_quality)
                            : item.release_quality;
                    }
                    if (item.release_date)        dataItem.release_date        = item.release_date;
                    if (item.last_air_date)       dataItem.last_air_date       = item.last_air_date;
                    if (item.last_episode_to_air) dataItem.last_episode_to_air = item.last_episode_to_air;
                    if (item.seasons)             dataItem.seasons             = item.seasons;
                    if (item.progress_marker)     dataItem.progress_marker     = item.progress_marker;
                    if (item.watched_count   !== undefined) dataItem.watched_count   = item.watched_count;
                    if (item.total_count     !== undefined) dataItem.total_count     = item.total_count;
                    if (item.released_count  !== undefined) dataItem.released_count  = item.released_count;

                    dataItem.promo_title = dataItem.title || dataItem.original_title;
                    dataItem.promo       = dataItem.overview;

                    return dataItem;
                }),
                page:          json.page || 1,
                total_pages:   json.total_pages || json.pagesCount || 1,
                total_results: json.total_results || json.total || 0
            };

            callback(normalized);
        }

        self.get = function (url, params, onComplete, onError) {
            self.network.silent(url, function (json) {
                if (!json) { onError(new Error('Empty response from server')); return; }
                normalizeData(json, function (normalized) { onComplete(normalized); });
            }, function (error) { onError(error); });
        };

        self.list = function (params, onComplete, onError) {
            params    = params    || {};
            onComplete = onComplete || function () {};
            onError    = onError    || function () {};

            var category = params.url;
            var page     = params.page || 1;
            var url      = buildUrl(category, page);

            if (!url) {
                onError(new Error('Не удалось построить URL. Проверьте настройки MediaHub.'));
                return;
            }

            self.get(url, params, function (json) {
                onComplete({
                    results:       json.results || [],
                    page:          json.page || page,
                    total_pages:   json.total_pages || 1,
                    total_results: json.total_results || 0
                });
            }, onError);
        };

        self.full = function (params, onSuccess, onError) {
            var card = params.card;
            params.method = !!(card.number_of_seasons || card.seasons || card.last_episode_to_air || card.first_air_date) ? 'tv' : 'movie';
            Lampa.Api.sources.tmdb.full(params, onSuccess, onError);
        };

        self.search = function (params, onComplete, onError) {
            params    = params    || {};
            onComplete = onComplete || function () {};
            onError    = onError    || function () {};

            var query = params.query || '';
            if (!BASE_URL || !query) { onError(new Error('no query')); return; }

            var url = BASE_URL + '/api/search?q=' + encodeURIComponent(query);
            var token = Lampa.Storage.get('mediahub_api_key', '');
            if (token) url += '&token=' + encodeURIComponent(token);

            self.get(url, params, function (json) {
                onComplete({
                    results:       json.results || [],
                    page:          1,
                    total_pages:   1,
                    total_results: json.total_results || 0
                });
            }, onError);
        };

        self.category = function (params, onSuccess, onError) {
            params = params || {};
            var partsData = [];

            var allCategories = getAllCategories();
            var menuOrder = getProfileSetting('mediahub_menu_sort', []);

            if (!Array.isArray(menuOrder) || menuOrder.length === 0) {
                menuOrder = [];
                for (var i = 0; i < allCategories.length; i++) menuOrder.push(allCategories[i].key);
                setProfileSetting('mediahub_menu_sort', menuOrder);
            }

            var menuHide = getProfileSetting('mediahub_menu_hide', []);

            // Build actual order: saved order + new categories appended
            var actualOrder = [];
            for (var i = 0; i < menuOrder.length; i++) {
                var k = menuOrder[i];
                for (var j = 0; j < allCategories.length; j++) {
                    if (allCategories[j].key === k) { actualOrder.push(k); break; }
                }
            }
            for (var j = 0; j < allCategories.length; j++) {
                var found = false;
                for (var k2 = 0; k2 < actualOrder.length; k2++) {
                    if (actualOrder[k2] === allCategories[j].key) { found = true; break; }
                }
                if (!found) actualOrder.push(allCategories[j].key);
            }

            for (var idx = 0; idx < actualOrder.length; idx++) {
                var key = actualOrder[idx];

                // Check hidden
                var isHidden = false;
                for (var h = 0; h < menuHide.length; h++) {
                    if (menuHide[h] === key) { isHidden = true; break; }
                }
                if (isHidden) continue;

                // Find category
                var cat = null;
                for (var j2 = 0; j2 < allCategories.length; j2++) {
                    if (allCategories[j2].key === key) { cat = allCategories[j2]; break; }
                }
                if (!cat) continue;

                // ── MyShows built-in ──────────────────────────────────────────
                if (key === 'myshows_unwatched') {
                    if (!window.MyShows || !window.MyShows.getUnwatchedShowsWithDetails) continue;
                    (function (title) {
                        partsData.push(function (callback) {
                            window.MyShows.getUnwatchedShowsWithDetails(function (response) {
                                if (response.error || !response.shows || !response.shows.length) {
                                    callback({ skip: true }); return;
                                }
                                var PAGE_SIZE = 20;
                                callback({
                                    title:       title,
                                    results:     response.shows.slice(0, PAGE_SIZE),
                                    url:         'myshows://unwatched',
                                    total_pages: Math.ceil(response.shows.length / PAGE_SIZE) || 1
                                });
                            });
                        });
                    })(cat.title);
                    continue;
                }

                // ── Episodes built-in ─────────────────────────────────────────
                if (key === 'episodes') {
                    var addEp = Lampa.Manifest.app_digital >= 300 ? addEpisodesV3 : addEpisodesV2;
                    addEp(partsData, cat.title, Lampa.TimeTable.lately);
                    continue;
                }

                if (key === 'recent') {
                    if (Lampa.Manifest.app_digital >= 300) addEpisodesV3(partsData, cat.title, Lampa.TimeTable.recently);
                    continue;
                }

                // ── Continues built-in ────────────────────────────────────────
                if (key === 'continues') {
                    if (Lampa.Manifest.app_digital >= 300) addContinues(partsData, cat.title, Lampa.Favorite.continues, undefined);
                    continue;
                }
                if (key === 'continues_movie') {
                    if (Lampa.Manifest.app_digital >= 300) addContinues(partsData, cat.title, Lampa.Favorite.continues, 'movie');
                    continue;
                }
                if (key === 'continues_tv') {
                    if (Lampa.Manifest.app_digital >= 300) addContinues(partsData, cat.title, Lampa.Favorite.continues, 'tv');
                    continue;
                }
                if (key === 'continues_anime') {
                    if (Lampa.Manifest.app_digital >= 300) addContinues(partsData, cat.title, Lampa.Favorite.continues, 'anime');
                    continue;
                }

                // ── Our API categories ────────────────────────────────────────
                (function (catKey, catTitle) {
                    partsData.push(function (callback) {
                        makeRequest(catKey, catTitle, callback);
                    });
                })(key, cat.title);
            }

            // ── Episode helpers ───────────────────────────────────────────────

            function addEpisodesV2(partsData, title) {
                partsData.push(function (callback) {
                    callback({
                        source: 'tmdb', results: Lampa.TimeTable.lately().slice(0, 20),
                        title: title, nomore: true,
                        cardClass: function (elem, params) { return new Episode(elem, params); }
                    });
                });
            }

            function addEpisodesV3(partsData, title, getFunc) {
                partsData.push(function (callback) {
                    var results = getFunc().slice(0, 20);
                    results.forEach(function (item) {
                        item.params = {
                            createInstance: function (data) {
                                return Lampa.Maker.make('Episode', data, function (m) { return m.only('Card', 'Callback'); });
                            },
                            emit: {
                                onlyEnter: function () { Lampa.Router.call('full', item.card); },
                                onlyFocus: function () { Lampa.Background.change(Lampa.Utils.cardImgBackgroundBlur(item.card)); }
                            }
                        };
                        Lampa.Arrays.extend(item, item.episode);
                    });
                    callback({ source: 'tmdb', results: results, title: title, nomore: true });
                });
            }

            function addContinues(partsData, title, getFunc, type) {
                partsData.push(function (callback) {
                    var all = (type !== undefined ? getFunc(type) : getFunc());
                    // Extra guard: Lampa sometimes returns wrong type items
                    var results = type ? all.filter(function(i) {
                        if (type === 'movie') return i.type === 'movie' || !!i.title;
                        if (type === 'tv')    return i.type === 'tv' || !!i.name;
                        if (type === 'anime') return i.type === 'tv' || !!i.name;
                        return true;
                    }) : all;
                    results = results.slice(0, 20);
                    results.forEach(function (item) {
                        if (item.first_air_date && !item.release_date) {
                            item.release_date = item.first_air_date;
                        }
                        item.params = {
                            createInstance: function (data) {
                                return Lampa.Maker.make('Card', data, function (m) { return m.only('Card', 'Release', 'Callback'); });
                            },
                            emit: { onlyEnter: function () { Lampa.Router.call('full', item); } }
                        };
                    });
                    callback({ source: 'tmdb', results: results, title: title, nomore: true });
                });
            }

            // ── API request ───────────────────────────────────────────────────

            function makeRequest(category, title, callback) {
                var url = buildUrl(category, 1);
                if (!url) { callback({ skip: true }); return; }

                self.get(url, params, function (json) {
                    var results = json.results || [];
                    if (window.MyShows && window.MyShows.prepareProgressMarkers) {
                        var prep = window.MyShows.prepareProgressMarkers({ results: results });
                        results = prep.results || prep.shows || results;
                    }
                    callback({
                        url:           category,
                        title:         title,
                        page:          1,
                        total_results: json.total_results || 0,
                        total_pages:   json.total_pages   || 1,
                        more:          (json.total_pages || 1) > 1,
                        results:       results,
                        source:        getProfileSetting('mediahub_source_name', DEFAULT_SOURCE_NAME),
                        _original_total_results: json.total_results || 0,
                        _original_total_pages:   json.total_pages   || 1,
                        _original_results:       json.results       || []
                    });
                }, function (error) { callback({ error: error }); });
            }

            function loadPart(partLoaded, partEmpty) {
                Lampa.Api.partNext(partsData, 5, partLoaded, partEmpty);
            }

            loadPart(onSuccess, onError);
            return loadPart;
        };
    }

    // ── Episode card class ────────────────────────────────────────────────────────

    function Episode(data) {
        var self = this;
        var card    = data.card || data;
        var episode = data.next_episode_to_air || data.episode || {};
        if (card.source === undefined) card.source = SOURCE_NAME;
        Lampa.Arrays.extend(card, {
            title:          card.name,
            original_title: card.original_name,
            release_date:   card.first_air_date
        });
        card.release_year = ((card.release_date || '0000') + '').slice(0, 4);

        function remove(elem) { if (elem) elem.remove(); }

        self.build = function () {
            self.card = Lampa.Template.js('card_episode');
            if (!self.card) { Lampa.Noty.show('Error: card_episode template not found'); return; }
            self.img_poster  = self.card.querySelector('.card__img') || {};
            self.img_episode = self.card.querySelector('.full-episode__img img') || {};
            self.card.querySelector('.card__title').innerText     = card.title || 'No title';
            self.card.querySelector('.full-episode__num').innerText = card.unwatched || '';
            if (episode && episode.air_date) {
                self.card.querySelector('.full-episode__name').innerText = 's' + (episode.season_number || '?') + 'e' + (episode.episode_number || '?') + '. ' + (episode.name || Lampa.Lang.translate('noname'));
                self.card.querySelector('.full-episode__date').innerText = Lampa.Utils.parseTime(episode.air_date).full;
            }
            if (card.release_year === '0000') remove(self.card.querySelector('.card__age'));
            else self.card.querySelector('.card__age').innerText = card.release_year;
            self.card.addEventListener('visible', self.visible);
        };

        self.image = function () {
            self.img_poster.onerror  = function () { self.img_poster.src  = './img/img_broken.svg'; };
            self.img_episode.onerror = function () { self.img_episode.src = './img/img_broken.svg'; };
            self.img_episode.onload  = function () { self.card.querySelector('.full-episode__img').classList.add('full-episode__img--loaded'); };
        };

        self.visible = function () {
            if      (card.poster_path)  self.img_poster.src = Lampa.Api.img(card.poster_path);
            else if (card.profile_path) self.img_poster.src = Lampa.Api.img(card.profile_path);
            else if (card.poster)       self.img_poster.src = card.poster;
            else if (card.img)          self.img_poster.src = card.img;
            else                        self.img_poster.src = './img/img_broken.svg';

            if      (card.still_path)   self.img_episode.src = Lampa.Api.img(episode.still_path, 'w300');
            else if (card.backdrop_path)self.img_episode.src = Lampa.Api.img(card.backdrop_path, 'w300');
            else if (episode.img)       self.img_episode.src = episode.img;
            else if (card.img)          self.img_episode.src = card.img;
            else                        self.img_episode.src = './img/img_broken.svg';

            if (self.onVisible) self.onVisible(self.card, card);
        };

        self.create = function () {
            self.build();
            self.card.addEventListener('hover:focus', function () { if (self.onFocus) self.onFocus(self.card, card); });
            self.card.addEventListener('hover:hover', function () { if (self.onHover) self.onHover(self.card, card); });
            self.card.addEventListener('hover:enter', function () { if (self.onEnter) self.onEnter(self.card, card); });
            self.image();
        };

        self.destroy = function () {
            self.img_poster.onerror = self.img_poster.onload = function () {};
            self.img_episode.onerror = self.img_episode.onload = function () {};
            self.img_poster.src = self.img_episode.src = '';
            remove(self.card);
            self.card = self.img_poster = self.img_episode = null;
        };

        self.render = function (js) { return js ? self.card : $(self.card); };
    }

    // ── Profile helpers ───────────────────────────────────────────────────────────

    function getProfileId() {
        if (window.profiles_plugin) {
            var id = Lampa.Storage.get('lampac_profile_id', '');
            if (id) return String(id);
        }
        try {
            if (Lampa.Account.Permit.account && Lampa.Account.Permit.account.profile && Lampa.Account.Permit.account.profile.id) {
                return String(Lampa.Account.Permit.account.profile.id);
            }
        } catch (e) {}
        return '';
    }

    function getProfileKey(baseKey) {
        var id = getProfileId();
        return id ? baseKey + '_profile_' + id : baseKey;
    }

    function getProfileName() {
        var name = Lampa.Storage.get('lm_profile_name', '');
        if (name) return String(name);
        try {
            if (Lampa.Account.Permit.account && Lampa.Account.Permit.account.profile && Lampa.Account.Permit.account.profile.name) {
                return String(Lampa.Account.Permit.account.profile.name);
            }
        } catch (e) {}
        return '';
    }

    function getProfileSetting(key, defaultValue) {
        return Lampa.Storage.get(getProfileKey(key), defaultValue);
    }

    var _syncApplying = false;

    function setProfileSetting(key, value, sync) {
        Lampa.Storage.set(getProfileKey(key), value);
        if (sync !== false && !_syncApplying && window.__NMSync) {
            window.__NMSync.patch('mediahub', getProfileKey(key), value);
        }
    }

    function _baseKey(profileKey) {
        var idx = profileKey.lastIndexOf('_profile_');
        return idx >= 0 ? profileKey.slice(0, idx) : profileKey;
    }

    function _applyLmSetting(profileKey, value) {
        _syncApplying = true;
        Lampa.Storage.set(profileKey, value);
        var base = _baseKey(profileKey);
        if (getProfileKey(base) === profileKey) Lampa.Storage.set(base, value);
        _syncApplying = false;
    }

    function hasProfileSetting(key) {
        return window.localStorage.getItem(getProfileKey(key)) !== null;
    }

    function loadMediaHubProfileSettings() {
        if (!hasProfileSetting('mediahub_hide_watched'))  setProfileSetting('mediahub_hide_watched',  'true',               false);
        if (!hasProfileSetting('mediahub_min_progress'))  setProfileSetting('mediahub_min_progress',  DEFAULT_MIN_PROGRESS, false);
        if (!hasProfileSetting('mediahub_source_name'))   setProfileSetting('mediahub_source_name',   DEFAULT_SOURCE_NAME,  false);
        if (!hasProfileSetting('mediahub_menu_sort'))     setProfileSetting('mediahub_menu_sort',     [],                   false);
        if (!hasProfileSetting('mediahub_menu_hide'))     setProfileSetting('mediahub_menu_hide',     [],                   false);
        if (!hasProfileSetting('mediahub_quality_mode'))  setProfileSetting('mediahub_quality_mode',  'simple',             false);

        Lampa.Storage.set('mediahub_hide_watched', getProfileSetting('mediahub_hide_watched', 'true'));
        Lampa.Storage.set('mediahub_min_progress', getProfileSetting('mediahub_min_progress', DEFAULT_MIN_PROGRESS));
        Lampa.Storage.set('mediahub_source_name',  getProfileSetting('mediahub_source_name',  DEFAULT_SOURCE_NAME));
        Lampa.Storage.set('mediahub_menu_sort',    getProfileSetting('mediahub_menu_sort',    []));
        Lampa.Storage.set('mediahub_menu_hide',    getProfileSetting('mediahub_menu_hide',    []));
        Lampa.Storage.set('mediahub_quality_mode', getProfileSetting('mediahub_quality_mode', 'simple'));
    }

    // ── Menu editor ───────────────────────────────────────────────────────────────

    function openMediaHubMenuEditor() {
        var allCategories = getAllCategories();
        var savedOrder = getProfileSetting('mediahub_menu_sort', []);
        if (!Array.isArray(savedOrder) || savedOrder.length === 0) {
            savedOrder = allCategories.map(function (c) { return c.key; });
            setProfileSetting('mediahub_menu_sort', savedOrder);
        }
        var savedHide = getProfileSetting('mediahub_menu_hide', []);

        var ordered = [];
        for (var i = 0; i < savedOrder.length; i++) {
            for (var j = 0; j < allCategories.length; j++) {
                if (allCategories[j].key === savedOrder[i]) { ordered.push(allCategories[j]); break; }
            }
        }
        for (var j = 0; j < allCategories.length; j++) {
            var exists = false;
            for (var k = 0; k < ordered.length; k++) {
                if (ordered[k].key === allCategories[j].key) { exists = true; break; }
            }
            if (!exists) ordered.push(allCategories[j]);
        }

        if (!document.getElementById('mh-ctrl-style')) {
            $('<style id="mh-ctrl-style">').text(
                '.menu-edit-list__ctrl{justify-content:center;opacity:.7;transition:opacity .15s;}' +
                '.menu-edit-list__ctrl.focus{opacity:1;background:rgba(255,255,255,.12);border-radius:.3em;}'
            ).appendTo('head');
        }

        function scrollItemCenter(el) {
            var body = el.parentNode;
            while (body && (body.className || '').indexOf('scroll__body') < 0) body = body.parentNode;
            if (!body) return;
            var content = body.parentNode;
            var viewH = content.clientHeight, bodyH = body.offsetHeight;
            var elR = el.getBoundingClientRect(), bodyR = body.getBoundingClientRect();
            var targetY = -(elR.top - bodyR.top) + (viewH - (elR.bottom - elR.top)) / 2;
            if (targetY > 0) targetY = 0;
            if (targetY < viewH - 50 - bodyH) targetY = viewH - 50 - bodyH;
            body.style.transform = 'translateY(' + targetY + 'px)';
        }

        var list = $('<div class="menu-edit-list"></div>');
        var btnEnableAll  = $('<div class="menu-edit-list__item menu-edit-list__ctrl selector">Включить все</div>');
        var btnDisableAll = $('<div class="menu-edit-list__item menu-edit-list__ctrl selector">Отключить все</div>');
        btnEnableAll.on('hover:enter',  function () { list.find('.dot').attr('opacity', '1'); });
        btnDisableAll.on('hover:enter', function () { list.find('.dot').attr('opacity', '0'); });
        list.append(btnEnableAll).append(btnDisableAll);

        var scrollTimer;
        ordered.forEach(function (cat) {
            var isVisible = savedHide.indexOf(cat.key) === -1;
            var item = $([
                '<div class="menu-edit-list__item">',
                '<div class="menu-edit-list__icon">' + ICON + '</div>',
                '<div class="menu-edit-list__title">' + cat.title + '</div>',
                '<div class="menu-edit-list__move move-up selector">',
                '<svg width="22" height="14" viewBox="0 0 22 14" fill="none"><path d="M2 12L11 3L20 12" stroke="currentColor" stroke-width="4" stroke-linecap="round"/></svg></div>',
                '<div class="menu-edit-list__move move-down selector">',
                '<svg width="22" height="14" viewBox="0 0 22 14" fill="none"><path d="M2 2L11 11L20 2" stroke="currentColor" stroke-width="4" stroke-linecap="round"/></svg></div>',
                '<div class="menu-edit-list__toggle toggle selector">',
                '<svg width="26" height="26" viewBox="0 0 26 26" fill="none">',
                '<rect x="1.89" y="1.78" width="21.79" height="21.79" rx="3.5" stroke="currentColor" stroke-width="3"/>',
                '<path d="M7.45 12.97L10.82 16.33L18.13 9.03" stroke="currentColor" stroke-width="3" class="dot" opacity="' + (isVisible ? 1 : 0) + '" stroke-linecap="round"/>',
                '</svg></div></div>'
            ].join('')).data('key', cat.key);

            item.find('.selector').on('hover:focus', function () {
                clearTimeout(scrollTimer);
                scrollTimer = setTimeout(function () { scrollItemCenter(item[0]); }, 0);
            });
            item.find('.move-up').on('hover:enter', function () {
                var prev = item.prev(':not(.menu-edit-list__ctrl)');
                if (prev.length) { item.insertBefore(prev); Lampa.Controller.toggle('modal'); scrollItemCenter(item[0]); }
            });
            item.find('.move-down').on('hover:enter', function () {
                var next = item.next(':not(.menu-edit-list__ctrl)');
                if (next.length) { item.insertAfter(next); Lampa.Controller.toggle('modal'); scrollItemCenter(item[0]); }
            });
            item.find('.toggle').on('hover:enter', function () {
                var dot = item.find('.dot');
                dot.attr('opacity', dot.attr('opacity') === '1' ? '0' : '1');
            });
            list.append(item);
        });

        Lampa.Modal.open({
            title: 'Порядок категорий',
            html:  list,
            size:  'small',
            onBack: function () {
                var newOrder = [], newHide = [];
                list.find('.menu-edit-list__item').each(function () {
                    var key = $(this).data('key');
                    if (!key) return;
                    newOrder.push(key);
                    if ($(this).find('.dot').attr('opacity') !== '1') newHide.push(key);
                });
                setProfileSetting('mediahub_menu_sort', newOrder);
                setProfileSetting('mediahub_menu_hide', newHide);
                Lampa.Modal.close();
                Lampa.Controller.toggle('settings_component');
            }
        });
    }

    // ── Settings ──────────────────────────────────────────────────────────────────

    function initSettings() {
        try { if (Lampa.SettingsApi.removeComponent) Lampa.SettingsApi.removeComponent('mediahub_settings'); } catch (e) {}

        Lampa.SettingsApi.addComponent({
            component: 'mediahub_settings',
            name:      SOURCE_NAME,
            icon:      ICON
        });

        Lampa.SettingsApi.addParam({
            component: 'mediahub_settings',
            param: { name: 'mediahub_edit_menu_order', type: 'button', title: 'Изменить порядок категорий' },
            field: { name: 'Порядок категорий', description: 'Перетаскивайте категории, чтобы изменить порядок и видимость' },
            onChange: function () { openMediaHubMenuEditor(); }
        });

        Lampa.SettingsApi.addParam({
            component: 'mediahub_settings',
            param: {
                name:    'mediahub_hide_watched',
                type:    'trigger',
                default: getProfileSetting('mediahub_hide_watched', 'true')
            },
            field: { name: 'Скрыть просмотренные', description: 'Скрывать просмотренные фильмы и сериалы' },
            onChange: function (value) {
                setProfileSetting('mediahub_hide_watched', value === true || value === 'true');
                MEDIAHUB_HIDE_WATCHED = value === true || value === 'true';
                var active = Lampa.Activity.active();
                if (active && active.activity_line && active.activity_line.listener && typeof active.activity_line.listener.send === 'function') {
                    active.activity_line.listener.send({ type: 'append', data: active.activity_line.card_data, line: active.activity_line });
                } else {
                    location.reload();
                }
            }
        });

        if (MEDIAHUB_HIDE_WATCHED) {
            Lampa.SettingsApi.addParam({
                component: 'mediahub_settings',
                param: {
                    name:    'mediahub_min_progress',
                    type:    'select',
                    values:  { '50':'50%','55':'55%','60':'60%','65':'65%','70':'70%','75':'75%','80':'80%','85':'85%','90':'90%','95':'95%','100':'100%' },
                    default: getProfileSetting('mediahub_min_progress', DEFAULT_MIN_PROGRESS).toString()
                },
                field: { name: 'Порог просмотра', description: 'Минимальный процент просмотра для скрытия контента' },
                onChange: function (value) {
                    newProgress = parseInt(value, 10);
                    setProfileSetting('mediahub_min_progress', newProgress);
                    MIN_PROGRESS = newProgress;
                }
            });

            Lampa.SettingsApi.addParam({
                component: 'mediahub_settings',
                param: {
                    name:        'mediahub_api_key',
                    type:        'input',
                    placeholder: 'Вставьте токен',
                    values:      '',
                    default:     Lampa.Storage.get('mediahub_api_key', '')
                },
                field: { name: 'Токен устройства', description: 'Токен для идентификации устройства. Получите на сайте или привяжите кнопкой ниже.' },
                onChange: function (value) { Lampa.Storage.set('mediahub_api_key', value); }
            });

            Lampa.SettingsApi.addParam({
                component: 'mediahub_settings',
                param: { name: 'mediahub_activate_device', type: 'button', title: 'Привязать устройство' },
                field: { name: 'Привязать устройство', description: 'Показать код для ввода на сайте — без ручного набора токена' },
                onChange: function () { startDeviceActivation(); }
            });
        }

        Lampa.SettingsApi.addParam({
            component: 'mediahub_settings',
            param: {
                name:        'mediahub_source_name',
                type:        'input',
                placeholder: 'Введите название',
                values:      '',
                default:     getProfileSetting('mediahub_source_name', DEFAULT_SOURCE_NAME)
            },
            field: { name: 'Название источника', description: 'Изменение названия источника в меню' },
            onChange: function (value) {
                newName = value;
                setProfileSetting('mediahub_source_name', value);
                $('.mh_menu_text').text(value);
                Lampa.Settings.update();
            }
        });

        Lampa.SettingsApi.addParam({
            component: 'mediahub_settings',
            param: {
                name:    'mediahub_quality_mode',
                type:    'select',
                values:  { 'full': 'Полное (WEBDL 1080p, BDRip…)', 'simple': 'Упрощённое (SD, 720p, 1080p, 4K)' },
                default: getProfileSetting('mediahub_quality_mode', 'simple')
            },
            field: { name: 'Формат качества', description: 'Как отображать качество видео' },
            onChange: function (value) { setProfileSetting('mediahub_quality_mode', value); }
        });
    }

    // ── Quality helper ────────────────────────────────────────────────────────────

    function getQuality(qualityStr) {
        if (!qualityStr || typeof qualityStr !== 'string') return qualityStr;
        var q = qualityStr.toLowerCase();
        if (q.indexOf('2160p') !== -1 || q.indexOf('4k') !== -1) return '4K';
        if (q.indexOf('1080p') !== -1) return '1080p';
        if (q.indexOf('720p')  !== -1) return '720p';
        return 'SD';
    }

    // ── Device activation ─────────────────────────────────────────────────────────

    (function () {
        if (document.getElementById('mh-act-styles')) return;
        var s = document.createElement('style');
        s.id = 'mh-act-styles';
        s.textContent = [
            '.mh-act-overlay{position:fixed;top:0;left:0;right:0;bottom:0;background:rgba(0,0,0,.92);z-index:9999;display:flex;align-items:center;justify-content:center}',
            '.mh-act-box{background:#1e1e2e;border-radius:1.8rem;padding:4rem 6rem;text-align:center;max-width:900px;width:85%;border:2px solid #ffffff15}',
            '.mh-act-title{font-size:2.4rem;font-weight:700;margin-bottom:1.8rem}',
            '.mh-act-url{color:#60a5fa;font-size:1.5rem;font-weight:600;margin-bottom:1.2rem;word-break:break-all}',
            '.mh-act-hint{color:#aaa;font-size:1.3rem;margin-bottom:.6rem}',
            '.mh-act-code{font-size:5rem;font-weight:800;letter-spacing:1rem;color:#4ade80;font-family:monospace;border:3px solid #4ade80;border-radius:1rem;padding:.8rem 2.5rem;display:inline-block;margin:1.5rem 0}',
            '.mh-act-timer{color:#888;font-size:1.1rem;margin-bottom:1rem}',
            '.mh-act-status{font-size:1.3rem;min-height:2rem}',
            '.mh-act-close{color:#555;font-size:1rem;margin-top:1.8rem}'
        ].join('');
        document.head.appendChild(s);
    })();

    function startDeviceActivation() {
        var overlay = null, pollTimer = null, countdown = null;

        function removeOverlay() {
            if (pollTimer)  { clearInterval(pollTimer);  pollTimer = null; }
            if (countdown)  { clearInterval(countdown);  countdown = null; }
            if (overlay)    { overlay.remove();           overlay = null; }
        }

        function setStatus(text, color) {
            if (overlay) overlay.find('.mh-act-status').css('color', color || '').text(text);
        }

        function showOverlay(code, expiresIn) {
            var remaining = expiresIn;
            overlay = $([
                '<div class="mh-act-overlay">',
                '<div class="mh-act-box">',
                '<div class="mh-act-title">Привязка устройства</div>',
                '<div class="mh-act-url">' + BASE_URL + '</div>',
                '<div class="mh-act-hint">Мои устройства → Привязать устройство</div>',
                '<div class="mh-act-hint">Введите этот код:</div>',
                '<div class="mh-act-code">' + code.replace(/(.{3})(.{3})/, '$1 $2') + '</div>',
                '<div class="mh-act-timer">Действует ' + remaining + ' сек.</div>',
                '<div class="mh-act-status">Ожидаю привязки…</div>',
                '<div class="mh-act-close">Нажмите Назад для отмены</div>',
                '</div></div>'
            ].join('')).appendTo('body');

            countdown = setInterval(function () {
                remaining--;
                if (!overlay) { clearInterval(countdown); return; }
                overlay.find('.mh-act-timer').text('Действует ' + remaining + ' сек.');
                if (remaining <= 0) {
                    clearInterval(countdown);
                    setStatus('Код истёк. Закройте и попробуйте снова.', '#f87171');
                    removeOverlay();
                }
            }, 1000);

            overlay.on('click', function (e) {
                if ($(e.target).hasClass('mh-act-overlay')) removeOverlay();
            });

            Lampa.Controller.add('mh_act', {
                back:   function () { removeOverlay(); Lampa.Controller.toggle('settings_component'); },
                toggle: function () {}
            });
            Lampa.Controller.toggle('mh_act');
        }

        function startPolling(code, interval) {
            pollTimer = setInterval(function () {
                fetch(BASE_URL + '/device/status?code=' + encodeURIComponent(code))
                    .then(function (r) { return r.json(); })
                    .then(function (data) {
                        if (data.linked && data.token) {
                            Lampa.Storage.set('mediahub_api_key', data.token);
                            setStatus('Устройство привязано!', '#4ade80');
                            Lampa.Noty.show('MediaHub: устройство привязано');
                            setTimeout(function () {
                                removeOverlay();
                                Lampa.Settings.update();
                                setTimeout(function () { location.reload(); }, 1000);
                            }, 2000);
                        }
                    })
                    .catch(function () {});
            }, (interval || 3) * 1000);
        }

        fetch(BASE_URL + '/device/code', { method: 'POST' })
            .then(function (r) { return r.json(); })
            .then(function (data) { showOverlay(data.code, data.expires_in); startPolling(data.code, data.poll_interval || 3); })
            .catch(function () { Lampa.Noty.show('MediaHub: не удалось получить код активации'); });
    }

    // ── Timecode sync ─────────────────────────────────────────────────────────────

    var _timecodeActive = false;
    var _lastSent = {};
    var SYNC_THROTTLE_MS = 15000;

    function getCurrentCard() {
        var active = Lampa.Activity && Lampa.Activity.active && Lampa.Activity.active();
        return (active && (active.card_data || active.card || active.movie)) || null;
    }

    function isMovieContent(card) {
        if (!card) return true;
        if (card.media_type === 'movie') return true;
        if (card.media_type === 'tv')   return false;
        if (card.number_of_seasons > 0) return false;
        var active = Lampa.Activity && Lampa.Activity.active && Lampa.Activity.active();
        if (active && active.method === 'tv') return false;
        return Boolean(!card.original_name && (card.original_title || card.title));
    }

    function setupTimecodeSync() {
        if (_timecodeActive) return;
        _timecodeActive = true;

        function tryAttach() {
            if (window.Lampa && Lampa.Timeline && Lampa.Timeline.listener) {
                Lampa.Timeline.listener.follow('update', onTimelineUpdate);
                Log.info('Timecode sync: attached');
            } else {
                setTimeout(tryAttach, 1000);
            }
        }
        tryAttach();
    }

    function onTimelineUpdate(data) {
        if (!data || !data.data || !data.data.hash || !data.data.road) return;

        var token = Lampa.Storage.get('mediahub_api_key', '');
        if (!token || !BASE_URL) return;

        var card = getCurrentCard();
        if (!card || !card.id) return;

        var hash     = String(data.data.hash);
        var road     = data.data.road;
        var percent  = Math.round(road.percent  || 0);
        var time     = Math.round(road.time     || 0);
        var duration = Math.round(road.duration || 0);

        var mt     = card.media_type || (isMovieContent(card) ? 'movie' : 'tv');
        var cardId = String(card.id) + '_' + mt;
        var key    = cardId + '::' + hash;
        var now    = Date.now();
        var last   = _lastSent[key];

        if (last && (now - last.sentAt < SYNC_THROTTLE_MS) && last.percent === percent) return;
        _lastSent[key] = { percent: percent, sentAt: now };

        Log.info('Timecode:', cardId, hash, percent + '%');

        var url = BASE_URL + '/timecode?token=' + encodeURIComponent(token);
        var profileId   = getProfileId();
        var profileName = getProfileName();
        if (profileId)   url += '&profile_id='   + encodeURIComponent(profileId);
        if (profileName) url += '&profile_name=' + encodeURIComponent(profileName);

        fetch(url, {
            method:  'POST',
            headers: { 'Content-Type': 'application/json' },
            body:    JSON.stringify({ card_id: cardId, item: hash, data: JSON.stringify({ time: time, duration: duration, percent: percent }) })
        }).catch(function (err) { Log.error('Timecode error:', err); });
    }

    // ── Plugin bootstrap ──────────────────────────────────────────────────────────

    function startPlugin() {
        if (window.mediahub_plugin) return;
        window.mediahub_plugin = true;

        // Patch category_full to skip empty pages
        var originalCategoryFull = Lampa.Component.get('category_full');
        if (originalCategoryFull) {
            Lampa.Component.add('category_full', function (object) {
                var comp = originalCategoryFull(object);
                var originalBuild = comp.build;
                comp.build = function (data) {
                    if (!data.results.length && object.source === SOURCE_NAME && data.total_pages > 1) {
                        object.page = (object.page || 1) + 1;
                        Lampa.Api.list(object, this.build.bind(this), this.empty.bind(this));
                        return;
                    }
                    originalBuild.call(this, data);
                };
                return comp;
            });
        }

        var mediahubApi = new MediaHubApiService();
        Lampa.Api.sources.mediahub = mediahubApi;
        Object.defineProperty(Lampa.Api.sources, SOURCE_NAME, {
            get: function () { return mediahubApi; }
        });

        newName = Lampa.Storage.get('mediahub_source_name', SOURCE_NAME);
        if (Lampa.Storage.field('start_page') === SOURCE_NAME) {
            window.start_deep_link = { component: 'category', page: 1, url: '', source: SOURCE_NAME, title: SOURCE_NAME };
        }

        var values = Lampa.Params.values.start_page;
        if (values) values[SOURCE_NAME] = SOURCE_NAME;

        var menuItem = $('<li data-action="mediahub" class="menu__item selector"><div class="menu__ico">' + ICON + '</div><div class="menu__text mh_menu_text">' + SOURCE_NAME + '</div></li>');
        $('.menu .menu__list').eq(0).append(menuItem);

        menuItem.on('hover:enter', function () {
            Lampa.Activity.push({ title: SOURCE_NAME, component: 'category', source: SOURCE_NAME, page: 1 });
        });

        // ── История MediaHub ──────────────────────────────────────────────────
        var HISTORY_ICON = '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor"><path d="M13 3a9 9 0 0 0-9 9H1l3.89 3.89.07.14L9 12H6c0-3.87 3.13-7 7-7s7 3.13 7 7-3.13 7-7 7c-1.93 0-3.68-.79-4.94-2.06l-1.42 1.42A8.954 8.954 0 0 0 13 21a9 9 0 0 0 0-18zm-1 5v5l4.28 2.54.72-1.21L13.5 13V8H12z"/></svg>';

        function fetchMhHistory(object, onDone, onFail) {
            var token = Lampa.Storage.get('mediahub_api_key', '');
            if (!token || !BASE_URL) { onFail(); return; }
            var profileId = object.profile_id !== undefined ? object.profile_id : getProfileId();
            var url = BASE_URL + '/timecode/history?token=' + encodeURIComponent(token) + '&page=' + (object.page || 1);
            if (profileId) url += '&profile_id=' + encodeURIComponent(profileId);
            fetch(url)
                .then(function (r) { return r.ok ? r.json() : Promise.reject(r.status); })
                .then(function (data) {
                    var results = (data.results || []).map(function (item) {
                        var isTv = item.media_type === 'tv';
                        var card = {
                            id:             item.tmdb_id,
                            type:           item.media_type,
                            original_title: item.original_title || '',
                            poster_path:    item.poster_path || '',
                            vote_average:   item.vote_average || 0,
                            _np_watched_ep: item.watched_episodes,
                            _np_total_ep:   item.total_episodes,
                        };
                        if (isTv) {
                            card.name           = item.title || item.original_title || '';
                            card.first_air_date = item.year ? item.year + '-01-01' : '';
                        } else {
                            card.title        = item.title || item.original_title || '';
                            card.release_date = item.year ? item.year + '-01-01' : '';
                        }
                        return card;
                    });
                    onDone({ results: results, total_pages: data.total_pages || 1 });
                })
                .catch(function () { onFail(); });
        }

        if (!Lampa.Component.get('mh_history')) {
            Lampa.Component.add('mh_history', function (object) {
                var comp = Lampa.Maker.make('Category', object, function (module) {
                    return module.toggle(module.MASK.base, 'Pagination');
                });
                comp.use({
                    onCreate: function () {
                        var self = this;
                        self.activity.loader(true);
                        fetchMhHistory(object, function (result) {
                            self.build(result);
                            self.activity.loader(false);
                        }, function () {
                            self.empty();
                            self.activity.loader(false);
                        });
                        Lampa.Listener.follow('profile', function (e) {
                            if (e.type !== 'changed') return;
                            object.profile_id = getProfileId();
                            object.page = 1;
                            self.activity.loader(true);
                            fetchMhHistory(object, function (r) { self.build(r); self.activity.loader(false); }, function () { self.empty(); self.activity.loader(false); });
                        });
                    },
                    onNext: function (resolve, reject) { fetchMhHistory(object, resolve, reject); },
                    onInstance: function (item, data) {
                        item.use({
                            onEnter: function () {
                                Lampa.Activity.push({ url: '', component: 'full', id: data.id, method: data.type === 'tv' ? 'tv' : 'movie', card: data });
                            },
                            onFocus: function () {
                                Lampa.Background.change(Lampa.Utils.cardImgBackground(data));
                            },
                        });
                    },
                });
                return comp;
            });
        }

        if (!$('.menu__item[data-action="mh_history"]').length) {
            var histMenuItem = $('<li data-action="mh_history" class="menu__item selector"><div class="menu__ico">' + HISTORY_ICON + '</div><div class="menu__text">История ' + SOURCE_NAME + '</div></li>');
            histMenuItem.on('hover:enter', function () {
                Lampa.Activity.push({ title: 'История ' + SOURCE_NAME, component: 'mh_history', page: 1, profile_id: getProfileId() });
            });
            $('.menu .menu__list').eq(0).append(histMenuItem);
        }

        function refreshProfileSettings() {
            loadMediaHubProfileSettings();
            setTimeout(function () {
                var panel = document.querySelector('[data-component="mediahub_settings"]');
                if (!panel) return;
                var hw = panel.querySelector('select[data-name="mediahub_hide_watched"]');
                if (hw) hw.value = getProfileSetting('mediahub_hide_watched', 'true');
                var mp = panel.querySelector('select[data-name="mediahub_min_progress"]');
                if (mp) mp.value = getProfileSetting('mediahub_min_progress', DEFAULT_MIN_PROGRESS).toString();
                var sn = panel.querySelector('input[data-name="mediahub_source_name"]');
                if (sn) sn.value = getProfileSetting('mediahub_source_name', DEFAULT_SOURCE_NAME);
                var qm = panel.querySelector('select[data-name="mediahub_quality_mode"]');
                if (qm) qm.value = getProfileSetting('mediahub_quality_mode', 'simple');
            }, 100);
        }

        Lampa.Listener.follow('profile', function (e) {
            if (e.type === 'changed') refreshProfileSettings();
        });
        Lampa.Listener.follow('state:changed', function (e) {
            if (e.target === 'favorite' && e.reason === 'profile') refreshProfileSettings();
        });

        setupTimecodeSync();
    }

    function initMediaHub() {
        startPlugin();
        MEDIAHUB_HIDE_WATCHED = Lampa.Storage.get('mediahub_hide_watched');

        setTimeout(function () {
            initSettings();
            loadMediaHubProfileSettings();

            if (window.__NMSync) {
                var SYNC_KEYS = [
                    'mediahub_hide_watched', 'mediahub_min_progress', 'mediahub_source_name',
                    'mediahub_menu_sort', 'mediahub_menu_hide', 'mediahub_quality_mode'
                ];
                window.__NMSync.register('mediahub', [], _applyLmSetting, function (serverKeys) {
                    SYNC_KEYS.forEach(function (key) {
                        var profileKey = getProfileKey(key);
                        if (serverKeys.indexOf(profileKey) < 0 && hasProfileSetting(key)) {
                            setProfileSetting(key, getProfileSetting(key));
                        }
                    });
                });
            }
        }, 50);
    }

    if (window.appready) {
        initMediaHub();
    } else {
        Lampa.Listener.follow('app', function (e) {
            if (e.type === 'ready') initMediaHub();
        });
    }

})();
