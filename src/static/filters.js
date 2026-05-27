(function () {
  var KEY = 'yavchn-blocked-domains';

  function load() {
    try {
      var raw = localStorage.getItem(KEY);
      if (!raw) return [];
      var arr = JSON.parse(raw);
      return Array.isArray(arr) ? arr.filter(function (s) { return typeof s === 'string' && s; }) : [];
    } catch (e) { return []; }
  }

  function save(arr) {
    try { localStorage.setItem(KEY, JSON.stringify(arr)); } catch (e) {}
  }

  // Normalize user input: strip protocol/path/whitespace, lowercase, drop a
  // leading "www." so `www.medium.com` and `medium.com` collapse to one entry.
  function normalize(input) {
    if (!input) return '';
    var s = input.trim().toLowerCase();
    s = s.replace(/^https?:\/\//, '');
    s = s.replace(/\/.*$/, '');
    s = s.replace(/^www\./, '');
    if (!/^[a-z0-9.-]+\.[a-z]{2,}$/.test(s)) return '';
    return s;
  }

  // Suffix match with a dot boundary: blocked "substack.com" matches
  // "substack.com" and "danluu.substack.com" but not "notsubstack.com".
  function hostMatches(host, blocked) {
    if (!host || !blocked) return false;
    host = host.toLowerCase();
    return host === blocked || host.endsWith('.' + blocked);
  }

  function apply() {
    var blocked = load();
    var rows = document.querySelectorAll('.pane-list .story[data-id]');
    if (!rows.length) return;
    for (var i = 0; i < rows.length; i++) {
      var host = hostOfRow(rows[i]);
      var hide = false;
      for (var j = 0; j < blocked.length; j++) {
        if (hostMatches(host, blocked[j])) { hide = true; break; }
      }
      rows[i].classList.toggle('filtered', hide);
    }
  }

  function hostOfRow(row) {
    var a = row.querySelector('.meta .host');
    if (!a || !a.href) return '';
    try { return new URL(a.href).hostname; } catch (e) { return ''; }
  }

  function renderList(dialog) {
    var ul = dialog.querySelector('.filters-list');
    var empty = dialog.querySelector('.filters-empty');
    if (!ul) return;
    var arr = load();
    ul.innerHTML = '';
    if (!arr.length) {
      if (empty) empty.hidden = false;
      return;
    }
    if (empty) empty.hidden = true;
    arr.sort();
    for (var i = 0; i < arr.length; i++) {
      var li = document.createElement('li');
      li.className = 'filters-list-item';
      var name = document.createElement('span');
      name.className = 'filters-list-name';
      name.textContent = arr[i];
      var btn = document.createElement('button');
      btn.type = 'button';
      btn.className = 'filters-list-remove';
      btn.setAttribute('aria-label', 'Unblock ' + arr[i]);
      btn.dataset.domain = arr[i];
      btn.textContent = '×';
      li.appendChild(name);
      li.appendChild(btn);
      ul.appendChild(li);
    }
  }

  // Dialog wiring -- mirrors help.js pattern.
  var dialog = document.getElementById('filters-dialog');
  var openBtn = document.querySelector('.filters-button');
  if (dialog && openBtn && typeof dialog.showModal === 'function') {
    openBtn.addEventListener('click', function () {
      renderList(dialog);
      if (!dialog.open) dialog.showModal();
    });
    var closeBtn = dialog.querySelector('.help-dialog-close');
    if (closeBtn) closeBtn.addEventListener('click', function () { dialog.close(); });
    dialog.addEventListener('click', function (e) {
      if (e.target === dialog) dialog.close();
    });

    // Add a domain via the inline form.
    var form = dialog.querySelector('.filters-add');
    if (form) {
      form.addEventListener('submit', function (e) {
        e.preventDefault();
        var input = form.querySelector('.filters-add-input');
        if (!input) return;
        var d = normalize(input.value);
        if (!d) {
          input.setCustomValidity('Enter a domain like example.com');
          input.reportValidity();
          return;
        }
        input.setCustomValidity('');
        var arr = load();
        if (arr.indexOf(d) < 0) {
          arr.push(d);
          save(arr);
        }
        input.value = '';
        renderList(dialog);
        apply();
      });
    }

    // Remove a domain via the × in each row.
    dialog.addEventListener('click', function (e) {
      var btn = e.target.closest('.filters-list-remove');
      if (!btn) return;
      var d = btn.dataset.domain;
      if (!d) return;
      var arr = load().filter(function (x) { return x !== d; });
      save(arr);
      renderList(dialog);
      apply();
    });
  }

  apply();
})();
