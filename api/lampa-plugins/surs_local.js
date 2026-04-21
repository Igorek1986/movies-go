(function () {
    'use strict';

    var BASE = (function () {
        var src = (document.currentScript && document.currentScript.src) || '';
        return src.replace(/\/surs_local\.js.*$/, '');
    })();

    function loadV3Plugins() {
        // MyShows интеграция встроена в myshows.js — surs_myshows.js отдельно не нужен.
        Lampa.Utils.putScriptAsync(
            [BASE + '/surs/v3/surs_nav_buttons.js',
             BASE + '/surs/v3/surs_strmngs_row.js'],
            function () { console.log('SURS nav and strmngs загружены (local).'); }
        );

        Lampa.Utils.putScriptAsync(
            [BASE + '/surs/v3/surs.js'],
            function () { console.log('SURS (v3) загружен (local).'); }
        );

        setTimeout(function () {
            if (!window.SursSelect || !window.SursSelect.__initialized) {
                Lampa.Utils.putScriptAsync(
                    [BASE + '/surs/surs_select.js'],
                    function () { console.log('SURS select загружен (local).'); }
                );
            }
        }, 2000);
    }

    if (window.appready) {
        loadV3Plugins();
    } else {
        Lampa.Listener.follow('app', function (e) {
            if (e.type === 'ready') loadV3Plugins();
        });
    }
})();
