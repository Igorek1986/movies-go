(function () {
    'use strict';

    var myshows_icon = '<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg"><rect x="3" y="7" width="18" height="12" rx="3" style="fill:none;stroke:currentColor;stroke-width:2"/><line x1="12" y1="5" x2="7" y2="1" style="fill:none;stroke:currentColor;stroke-width:2;stroke-linecap:round"/><line x1="12" y1="5" x2="17" y2="1" style="fill:none;stroke:currentColor;stroke-width:2;stroke-linecap:round"/><circle cx="12" cy="6" r="1" style="fill:currentColor;stroke:none"/></svg>';

    // Кнопка в навигационной строке SURS
    function addMyShowsButton() {
        if (!window.MyShows) return;
        var btn = {
            id: 'myshows_unwatched',
            title: 'MyShows',
            icon: myshows_icon,
            action: function () { window.MyShows.openPage(); }
        };
        if (typeof window.surs_addExternalButton === 'function') {
            window.surs_addExternalButton(btn);
        } else {
            window.surs_external_buttons = window.surs_external_buttons || [];
            window.surs_external_buttons.push(btn);
        }
    }

    // Строка карточек через hook combinedData SURS
    function chainCustomButtonsRow() {
        var _prev = window.surs_getCustomButtonsRow;
        window.surs_getCustomButtonsRow = function (partsData) {
            if (_prev) _prev(partsData);
            if (!window.MyShows) return;
            partsData.push(function (callback) {
                window.MyShows.getUnwatchedShowsWithDetails(function (result) {
                    callback({
                        results: result.shows || [],
                        title: 'Непросмотренные сериалы (MyShows)'
                    });
                });
            });
        };
    }

    function init() {
        addMyShowsButton();
        chainCustomButtonsRow();
    }

    if (window.appready) {
        init();
    } else {
        Lampa.Listener.follow('app', function (e) {
            if (e.type === 'ready') init();
        });
    }

})();
