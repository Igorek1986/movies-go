// Строит HTML для iframe.srcdoc, в котором выполняется расширение.
// sandbox="allow-scripts" без allow-same-origin (см. ExtensionHost.tsx) —
// у документа непрозрачный origin, нет доступа к cookie/DOM хоста. Весь
// обмен с хостом — через postMessage (протокол см. window.Ext ниже).
//
// scriptURL резолвится браузером относительно текущей страницы (srcdoc
// наследует base URL родителя) — поэтому расширения из ./plugins/ хранятся
// в БД как относительный путь ("/timecode_import_lampac.js"), а не с жёстко
// зашитым доменом (у проекта несколько доменов-зеркал).

// Палитра — литеральные копии :root/:root[data-theme='glass'] из
// web/src/styles/themes.scss. Не var(--color-*): у sandboxed-iframe (srcdoc,
// allow-scripts без allow-same-origin) свой документ с чистым :root, CSS
// custom properties хоста в него не наследуются — значения приходится
// продублировать. Если палитра в themes.scss поменяется — обновить и тут.
const THEME_PALETTES = {
  classic: {
    bg: '#0f1117', bgCard: '#1a1d27', bgInput: '#252836', border: '#2d3148',
    primary: '#6c63ff', primaryHover: '#5a52e0', text: '#e8eaf6', textMuted: '#8a8fa8',
    success: '#52c752', successRgb: '82, 199, 82', danger: '#e05252', dangerRgb: '224, 82, 82',
    radiusSm: '6px',
  },
  glass: {
    bg: '#16181c', bgCard: '#24262a', bgInput: '#2a2c31', border: '#323437',
    primary: '#568dff', primaryHover: '#3d75f0', text: 'rgba(255,255,255,0.95)', textMuted: 'rgba(255,255,255,0.55)',
    success: '#30d158', successRgb: '48, 209, 88', danger: '#ff3b30', dangerRgb: '255, 59, 48',
    radiusSm: '12px',
  },
}

export function buildExtensionShell(scriptURL: string): string {
  const escapedURL = JSON.stringify(scriptURL)
  const theme = document.documentElement.getAttribute('data-theme') === 'glass' ? THEME_PALETTES.glass : THEME_PALETTES.classic

  return `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<style>
  :root { color-scheme: dark; }
  * { box-sizing: border-box; }
  html, body {
    background: ${theme.bgCard};
  }
  body {
    margin: 0;
    padding: 0;
    font: 14px/1.5 -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
    color: ${theme.text};
  }
  body > * + *, form > * + * { margin-top: 8px; }
  p { margin: 0; color: ${theme.textMuted}; }
  .row { display: flex; gap: 8px; flex-wrap: wrap; }
  .row > * { flex: 1; min-width: 140px; }
  a, code { color: ${theme.primary}; }
  input, select, textarea {
    width: 100%;
    background: ${theme.bgInput};
    border: 1px solid ${theme.border};
    border-radius: ${theme.radiusSm};
    color: ${theme.text};
    padding: 8px 16px;
    font: inherit;
    transition: border-color 150ms ease;
  }
  input:focus, select:focus, textarea:focus { outline: none; border-color: ${theme.primary}; }
  input::placeholder, textarea::placeholder { color: ${theme.textMuted}; }
  input, select { height: 38px; }
  select { cursor: pointer; }
  textarea { font-family: 'Courier New', monospace; font-size: 12px; min-height: 100px; resize: vertical; }
  button {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    height: 38px;
    padding: 0 24px;
    border: none;
    border-radius: ${theme.radiusSm};
    background: ${theme.primary};
    color: #fff;
    font: inherit;
    font-weight: 600;
    cursor: pointer;
    transition: background 150ms ease;
  }
  button:hover:not(:disabled) { background: ${theme.primaryHover}; }
  button:disabled { opacity: 0.6; cursor: default; }
  .alert {
    padding: 8px 12px;
    border-radius: ${theme.radiusSm};
    font-weight: 600;
  }
  .alert:empty { display: none; }
  .alert-success { background: rgba(${theme.successRgb}, 0.15); color: ${theme.success}; }
  .alert-error { background: rgba(${theme.dangerRgb}, 0.15); color: ${theme.danger}; }
  .copied-hint {
    display: inline-block;
    margin-left: 8px;
    font-weight: 600;
    color: ${theme.success};
  }
</style>
</head>
<body>
<script>
(function () {
  var pending = {};
  var reqId = 0;

  // Любая необработанная ошибка в самом расширении (опечатка, исключение в
  // обработчике клика и т.п.) раньше просто пропадала — ни в интерфейсе, ни
  // у родителя не было и следа, выглядело как "кнопка ничего не делает".
  // Показываем её прямо в теле расширения, как только она случится — так
  // и разработчик расширения, и пользователь сразу видят, что пошло не так,
  // без обращения к консоли браузера. Тот же приём, что и "SCRIPT_CRASH" у
  // потока (dev/potok-media/src/utils/extensions/iframeHelper.ts).
  function showFatalError(message) {
    var el = document.getElementById('__ext_fatal_error__');
    if (!el) {
      el = document.createElement('div');
      el.id = '__ext_fatal_error__';
      el.className = 'alert alert-error';
      document.body.insertBefore(el, document.body.firstChild);
    }
    el.textContent = '⚠ Ошибка расширения: ' + message;
  }
  window.__extShowFatalError = showFatalError;
  window.addEventListener('error', function (e) {
    showFatalError(e.message || String(e.error));
  });
  window.addEventListener('unhandledrejection', function (e) {
    var reason = e.reason;
    showFatalError(reason && reason.message ? reason.message : String(reason));
  });

  window.addEventListener('message', function (e) {
    var m = e.data;
    if (!m || typeof m !== 'object') return;
    if (m.type === 'ext:api:reply' && pending[m.reqId]) {
      var cb = pending[m.reqId];
      delete pending[m.reqId];
      cb(m.error, m.result);
    }
  });

  // Таймаут — если ответ от родителя почему-либо не придёт (например, сама
  // страница ушла в фон/уснула), промис должен всё равно разрешиться, а не
  // повиснуть навсегда с кнопкой "Отправка…" без какой-либо обратной связи.
  var CALL_TIMEOUT_MS = 15000;

  function call(action, params) {
    return new Promise(function (resolve, reject) {
      var id = ++reqId;
      var timer = setTimeout(function () {
        delete pending[id];
        reject(new Error('Нет ответа от сервера, попробуйте ещё раз'));
      }, CALL_TIMEOUT_MS);
      pending[id] = function (err, result) {
        clearTimeout(timer);
        if (err) reject(new Error(err)); else resolve(result);
      };
      parent.postMessage({ type: 'ext:api', reqId: id, action: action, params: params || {} }, '*');
    });
  }

  function ready(meta) {
    parent.postMessage({ type: 'ext:ready', meta: meta || {} }, '*');
  }

  function resize() {
    var h = document.documentElement.scrollHeight;
    parent.postMessage({ type: 'ext:resize', height: h }, '*');
  }

  if (typeof ResizeObserver !== 'undefined') {
    new ResizeObserver(resize).observe(document.body);
  } else {
    setInterval(resize, 500);
  }

  window.Ext = { call: call, ready: ready };
})();
</script>
<script src=${escapedURL} onerror="window.__extShowFatalError && window.__extShowFatalError('не удалось загрузить файл расширения (' + ${escapedURL} + ')')"></script>
</body>
</html>`
}
