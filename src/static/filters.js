/* Domain filters, kept in this browser under yavchn-blocked-domains. A
   story whose host is a blocked domain, or a subdomain of one, is hidden
   from every list. The list is edited in PUDL token fields, one in the
   filters dialog, which the View menu opens, and one in Settings. Storage
   is the list; each field shows it, and a reader's edit in one is saved
   and drawn into the others, which a script's set() does without firing
   change, so the fields never echo each other. */
(function () {
  'use strict';
  var KEY = 'yavchn-blocked-domains';

  function load() {
    try {
      var arr = JSON.parse(window.yavchnStorage.getItem(KEY) || '[]');
      return Array.isArray(arr) ? arr.filter(function (s) { return typeof s === 'string' && s; }) : [];
    } catch (e) { return []; }
  }

  function save(arr) {
    try { window.yavchnStorage.setItem(KEY, JSON.stringify(arr)); } catch (e) {}
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

  var FIELD = '[data-domain-filters] input[name="domains"]';

  // Draws the saved list into every field; PUDL takes in a field that
  // arrived with a window first, so it has its tokens.
  function renderList() {
    document.querySelectorAll(FIELD).forEach(function (input) {
      if (window.pudlCombo && !window.pudlCombo.tokens(input)) window.pudlCombo.enhance(input.closest('[data-domain-filters]'));
      var t = window.pudlCombo && window.pudlCombo.tokens(input);
      if (t) t.set(load());
    });
  }

  function changed() {
    apply();
    document.dispatchEvent(new CustomEvent('yavchn:list-change'));
  }

  // Every value, typed, pasted or set from storage, is cleaned down to
  // its domain, and anything that is not one is refused.
  document.addEventListener('pudl:token-check', function (e) {
    if (!e.target.matches(FIELD)) return;
    var d = normalize(e.detail.value);
    if (d) e.detail.replace(d);
    else e.detail.refuse('Enter a domain, such as example.com, or paste a link.');
  });

  // A reader's edit: the field's values are the list.
  document.addEventListener('change', function (e) {
    if (!e.target.matches(FIELD)) return;
    var t = window.pudlCombo && window.pudlCombo.tokens(e.target);
    if (!t) return;
    save(t.values());
    document.querySelectorAll(FIELD).forEach(function (input) {
      var other = input !== e.target && window.pudlCombo.tokens(input);
      if (other) other.set(load());
    });
    changed();
  });
  renderList();
  document.addEventListener('pudl:window-open', renderList);

  // For the story applet's menu, which blocks the site of the story in front.
  window.yavchn.hiding = window.yavchn.hiding || {};
  window.yavchn.hiding.blockDomain = function (host) {
    var d = normalize(host);
    if (!d) return;
    var arr = load();
    if (arr.indexOf(d) < 0) { arr.push(d); save(arr); }
    renderList();
    changed();
  };

  window.addEventListener('storage', function (e) {
    if (e.key !== KEY) return;
    renderList();
    apply();
  });

  window.yavchn.onList(apply);
  document.addEventListener('yavchn:rows-appended', apply);
})();
