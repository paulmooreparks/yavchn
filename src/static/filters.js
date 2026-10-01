/* Domain filters, kept in this browser under yavchn-blocked-domains. A
   story whose host is a blocked domain, or a subdomain of one, is hidden
   from every list. The list of domains is edited in the filters dialog,
   which the View menu opens. */
(function () {
  'use strict';
  var KEY = 'yavchn-blocked-domains';

  function load() {
    try {
      var arr = JSON.parse(localStorage.getItem(KEY) || '[]');
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
    document.querySelectorAll('.story-list .story-row[data-id]').forEach(function (row) {
      var host = row.dataset.host || '';
      row.classList.toggle('filtered', blocked.some(function (b) { return hostMatches(host, b); }));
    });
  }

  var dialog = document.getElementById('filters-dialog');

  function renderList() {
    if (!dialog) return;
    var ul = dialog.querySelector('.filters-list');
    var empty = dialog.querySelector('.filters-empty');
    var arr = load().sort();
    ul.textContent = '';
    if (empty) empty.hidden = arr.length > 0;
    arr.forEach(function (d) {
      var li = document.createElement('li');
      li.className = 'filters-list-item';
      var name = document.createElement('span');
      name.className = 'filters-list-name';
      name.textContent = d;
      var btn = document.createElement('button');
      btn.type = 'button';
      btn.className = 'icon-btn filters-list-remove';
      btn.setAttribute('aria-label', 'Unblock ' + d);
      btn.dataset.domain = d;
      li.appendChild(name);
      li.appendChild(btn);
      ul.appendChild(li);
    });
  }

  if (dialog) {
    var form = dialog.querySelector('.filters-add');
    form.addEventListener('submit', function (e) {
      e.preventDefault();
      var input = form.querySelector('input[name="domain"]');
      var d = normalize(input.value);
      if (!d) {
        input.setCustomValidity('Enter a domain like example.com');
        input.reportValidity();
        return;
      }
      input.setCustomValidity('');
      var arr = load();
      if (arr.indexOf(d) < 0) { arr.push(d); save(arr); }
      input.value = '';
      renderList();
      apply();
    });
    form.querySelector('input[name="domain"]').addEventListener('input', function (e) {
      e.target.setCustomValidity('');
    });

    dialog.addEventListener('click', function (e) {
      var btn = e.target.closest('.filters-list-remove');
      if (!btn) return;
      save(load().filter(function (x) { return x !== btn.dataset.domain; }));
      renderList();
      apply();
    });
    renderList();
  }

  // For the story applet's menu, which blocks the site of the story in front.
  window.yavchn.hiding = window.yavchn.hiding || {};
  window.yavchn.hiding.blockDomain = function (host) {
    var d = normalize(host);
    if (!d) return;
    var arr = load();
    if (arr.indexOf(d) < 0) { arr.push(d); save(arr); }
    renderList();
    apply();
    document.dispatchEvent(new CustomEvent('yavchn:list-change'));
  };

  window.addEventListener('storage', function (e) {
    if (e.key !== KEY) return;
    renderList();
    apply();
  });

  window.yavchn.onList(apply);
  document.addEventListener('yavchn:rows-appended', apply);
})();
