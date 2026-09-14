// Расширение /profiles → «Расширения»: разовый импорт просмотренного из
// MyShows.ru (фильмы + сериалы) в выбранный профиль. Только чтение с
// MyShows — сюда, обратно ничего не отправляется, поэтому "импорт", не
// "синхронизация". Выполняется в песочнице (sandboxed iframe, см.
// web/src/components/extensions/), доступ к бэкенду — только через
// window.Ext.call(action, params). Прогресс — через третий аргумент
// (onProgress), см. runStreamingAction в ExtensionHost.tsx: сам запрос
// идёт на хост-странице (у неё есть сессионная кука), сюда прилетают
// только уже распарсенные чанки SSE-эндпоинта /myshows/sync.
(function () {
  'use strict';

  var devicesCache = [];

  function el(tag, props, children) {
    var e = document.createElement(tag);
    if (props) {
      Object.keys(props).forEach(function (k) {
        if (k === 'style') Object.assign(e.style, props[k]);
        else if (k.indexOf('on') === 0) e.addEventListener(k.slice(2).toLowerCase(), props[k]);
        else e.setAttribute(k, props[k]);
      });
    }
    (children || []).forEach(function (c) { e.appendChild(typeof c === 'string' ? document.createTextNode(c) : c); });
    return e;
  }

  function option(value, label) {
    var o = document.createElement('option');
    o.value = value;
    o.textContent = label;
    return o;
  }

  function formatEntry(entry) {
    if (entry.type === 'stage') {
      var label = entry.stage === 'movies' ? 'Фильмы' : 'Сериалы';
      var name = entry.name ? ' — ' + entry.name : '';
      return label + ': ' + entry.current + '/' + entry.total + name;
    }
    return entry.message || '';
  }

  function render() {
    var hint = el('p', {}, ['Разовый перенос уже просмотренного в MyShows.ru (фильмы и сериалы) в выбранный профиль — таймкодами и карточками. Импорт идёт только оттуда сюда, обратно в MyShows ничего не отправляется.']);

    var status = el('div', { class: 'alert' }, []);
    var progressLine = el('div', { class: 'alert', style: { display: 'none' } }, []);
    var errorList = el('div', {}, []);

    var deviceSelect = el('select', {}, [option('', '— выберите —')]);
    var profileSelect = el('select', {}, [option('', 'Основной')]);
    var newDeviceName = el('input', { placeholder: 'Название устройства', style: { display: 'none' } });
    var newProfileName = el('input', { placeholder: 'Название профиля', style: { display: 'none' } });
    var loginInput = el('input', { placeholder: 'Логин MyShows', autocomplete: 'username' });
    var passwordInput = el('input', { type: 'password', placeholder: 'Пароль MyShows', autocomplete: 'current-password' });
    var submitBtn = el('button', { type: 'submit' }, ['Импортировать']);

    function refreshDeviceOptions() {
      deviceSelect.innerHTML = '';
      deviceSelect.appendChild(option('', '— выберите —'));
      devicesCache.forEach(function (d) { deviceSelect.appendChild(option(String(d.id), d.name)); });
      deviceSelect.appendChild(option('new', '＋ Новое устройство'));
    }

    function refreshProfileOptions(profiles) {
      profileSelect.innerHTML = '';
      profileSelect.appendChild(option('', 'Основной'));
      (profiles || []).forEach(function (p) { profileSelect.appendChild(option(p.profile_id, p.name)); });
      profileSelect.appendChild(option('new', '＋ Новый профиль'));
    }

    Ext.call('devices.list').then(function (r) {
      devicesCache = r.devices || [];
      refreshDeviceOptions();
    });

    deviceSelect.addEventListener('change', function () {
      newDeviceName.style.display = deviceSelect.value === 'new' ? 'inline-block' : 'none';
      profileSelect.innerHTML = '';
      profileSelect.appendChild(option('', 'Основной'));
      if (deviceSelect.value && deviceSelect.value !== 'new') {
        Ext.call('profiles.list', { deviceId: Number(deviceSelect.value) }).then(function (r) {
          refreshProfileOptions(r.profiles);
        });
      }
    });

    profileSelect.addEventListener('change', function () {
      newProfileName.style.display = profileSelect.value === 'new' ? 'inline-block' : 'none';
    });

    var form = el('form', {
      onSubmit: function (e) {
        e.preventDefault();
        status.className = 'alert';
        status.textContent = '';
        progressLine.style.display = 'none';
        progressLine.textContent = '';
        errorList.innerHTML = '';

        if (!deviceSelect.value) {
          status.className = 'alert alert-error';
          status.textContent = 'Выберите устройство';
          return;
        }
        if (!loginInput.value || !passwordInput.value) {
          status.className = 'alert alert-error';
          status.textContent = 'Укажите логин и пароль MyShows';
          return;
        }

        submitBtn.disabled = true;
        submitBtn.textContent = 'Импорт…';

        var deviceStep = deviceSelect.value === 'new'
          ? Ext.call('devices.create', { name: newDeviceName.value || 'Устройство' }).then(function (d) { return d.id; })
          : Promise.resolve(Number(deviceSelect.value));

        deviceStep.then(function (deviceId) {
          var profileStep = profileSelect.value === 'new'
            ? Ext.call('profiles.create', { deviceId: deviceId, name: newProfileName.value || 'Профиль' }).then(function (p) { return p.profile_id; })
            : Promise.resolve(profileSelect.value);
          return profileStep.then(function (profileId) {
            return Ext.call('myshows.sync', {
              deviceId: deviceId,
              profileId: profileId,
              login: loginInput.value,
              password: passwordInput.value,
            }, function onProgress(chunk) {
              if (chunk.type === 'error') {
                var line = el('div', { class: 'alert alert-error' }, [formatEntry(chunk)]);
                errorList.appendChild(line);
                return;
              }
              progressLine.style.display = 'block';
              progressLine.className = 'alert';
              progressLine.textContent = formatEntry(chunk);
            });
          });
        }).then(function () {
          progressLine.style.display = 'none';
          if (errorList.children.length === 0) {
            status.className = 'alert alert-success';
            status.textContent = '✓ Импорт завершён';
            loginInput.value = '';
            passwordInput.value = '';
          } else {
            status.className = 'alert';
            status.textContent = 'Импорт завершён с ошибками ниже';
          }
        }).catch(function (err) {
          progressLine.style.display = 'none';
          status.className = 'alert alert-error';
          var message = String(err.message || err);
          if (message === 'premium required') message = 'Импорт из MyShows доступен для подписчиков Premium.';
          status.textContent = '✗ ' + message;
        }).finally(function () {
          submitBtn.disabled = false;
          submitBtn.textContent = 'Импортировать';
        });
      },
    }, [
      el('div', { class: 'row' }, [deviceSelect, profileSelect]),
      el('div', { class: 'row' }, [newDeviceName, newProfileName]),
      el('div', { class: 'row' }, [loginInput, passwordInput]),
      submitBtn,
    ]);

    document.body.appendChild(hint);
    document.body.appendChild(status);
    document.body.appendChild(progressLine);
    document.body.appendChild(form);
    document.body.appendChild(errorList);
  }

  render();
  Ext.ready({ title: 'Импорт из MyShows' });
})();
