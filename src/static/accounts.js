/* Account-aware storage keeps anonymous data separate from synchronized data. */
(function () {
  'use strict';
  var keys = { 'yavchn-pinned': 'pins', 'yavchn-blocked-domains': 'domains', 'yavchn-applet-state': 'progress',
    'yavchn-collections': 'collections', 'yavchn-collected': 'collected', 'yavchn-notes': 'notes' };
  var KINDS = ['pins', 'domains', 'progress', 'collections', 'collected', 'notes'];
  function emptyData() { return { pins: {}, domains: [], progress: {}, collections: {}, collected: {}, notes: {} }; }
  var boot = document.getElementById('yavchn-account-data');
  var user = boot ? JSON.parse(boot.textContent) : null;
  var csrf = document.querySelector('meta[name="yavchn-csrf"]');
  var baseline = user && user.data, revision = user && user.revision;
  var dataURL = user && user.links.self.href;
  var syncStatus = 'Account data loaded.';
  var timer = 0, running = false, applying = false, memory = Object.create(null);
  function copy(value) { return JSON.parse(JSON.stringify(value)); }
  function ordered(value) {
    if (Array.isArray(value)) return value.map(ordered);
    if (value && typeof value === 'object') {
      var out = Object.create(null);
      Object.keys(value).sort().forEach(function (key) { out[key] = ordered(value[key]); });
      return out;
    }
    return value;
  }
  function same(a, b) { return JSON.stringify(ordered(a)) === JSON.stringify(ordered(b)); }
  function rawGet(key) {
    if (Object.prototype.hasOwnProperty.call(memory, key)) return memory[key];
    try { return localStorage.getItem(key); } catch (e) { return null; }
  }
  function rawSet(key, value) {
    try { localStorage.setItem(key, value); delete memory[key]; } catch (e) { memory[key] = value; }
  }
  function physical(key) { return user && keys[key] ? 'yavchn-account:' + user.id + ':' + keys[key] : key; }
  function documentData() {
    var out = emptyData();
    Object.keys(keys).forEach(function (key) { try { out[keys[key]] = JSON.parse(rawGet(physical(key))) || out[keys[key]]; } catch (e) {} });
    return out;
  }
  // A document stored before collections and notes lacks their members.
  function complete(data) {
    var out = emptyData();
    KINDS.forEach(function (kind) { if (data && data[kind]) out[kind] = data[kind]; });
    return out;
  }
  function domainsMap(list) { var map = Object.create(null); list.forEach(function (v) { map[v] = true; }); return map; }
  // Only entries changed locally are applied to the newer remote representation.
  function rebase(before, after, remote) {
    before = complete(before); after = complete(after);
    var out = complete(copy(remote));
    KINDS.forEach(function (kind) {
      var left = kind === 'domains' ? domainsMap(before[kind]) : before[kind];
      var right = kind === 'domains' ? domainsMap(after[kind]) : after[kind];
      var target = kind === 'domains' ? domainsMap(out[kind]) : out[kind];
      var entries = new Set(Object.keys(left).concat(Object.keys(right)));
      entries.forEach(function (key) {
        if (same(left[key], right[key])) return;
        if (Object.prototype.hasOwnProperty.call(right, key)) target[key] = copy(right[key]);
        else delete target[key];
      });
      out[kind] = kind === 'domains' ? Object.keys(target).sort() : target;
    });
    // Match the reader's existing retention limits after combining devices.
    [['pins', 500, 'pinned_at'], ['progress', 100, 't'], ['collections', 100, 'created_at'],
      ['collected', 2000, 'added_at'], ['notes', 1000, 'updated_at']].forEach(function (limit) {
      var map = out[limit[0]], names = Object.keys(map);
      names.sort(function (a, b) { return (map[b][limit[2]] || 0) - (map[a][limit[2]] || 0); });
      names.slice(limit[1]).forEach(function (name) { delete map[name]; });
    });
    // A collection deleted on one device takes the entries another device added to it.
    Object.keys(out.collected).forEach(function (key) {
      if (!Object.prototype.hasOwnProperty.call(out.collections, key.split(':')[0])) delete out.collected[key];
    });
    return out;
  }
  function status(text) {
    syncStatus = text;
    document.querySelectorAll('[data-account-sync-status]').forEach(function (el) { el.textContent = text; });
    document.querySelectorAll('[data-account-link]').forEach(function (el) { el.title = text; });
  }
  function install(data, notify) {
    applying = true;
    Object.keys(keys).forEach(function (key) {
      var value = JSON.stringify(data[keys[key]]), old = rawGet(physical(key));
      rawSet(physical(key), value);
      if (notify && old !== value) window.dispatchEvent(new StorageEvent('storage', { key: key, newValue: value }));
    });
    applying = false;
  }
  function rememberBase() { rawSet('yavchn-account:' + user.id + ':baseline', JSON.stringify(baseline)); }
  function schedule() {
    if (!user || applying) return;
    clearTimeout(timer);
    status('Changes are waiting to synchronize.');
    timer = setTimeout(function () { synchronize(); }, 1200);
  }
  window.yavchnStorage = {
    getItem: function (key) { return rawGet(physical(key)); },
    setItem: function (key, value) { rawSet(physical(key), value); if (keys[key]) schedule(); }
  };
  window.addEventListener('pageshow', function (event) { if (event.persisted) location.reload(); });
  window.yavchnAccountUI = function (root) {
    if (user) root.querySelectorAll('[data-account-sync-status]').forEach(function (el) { el.textContent = syncStatus; });
    root.querySelectorAll('[data-account-import]').forEach(function (button) {
      if (!user || button.dataset.accountBound) return;
      button.dataset.accountBound = "true";
      button.hidden = false;
      button.addEventListener('click', function () {
        var data = documentData(), anonymous = emptyData();
        Object.keys(keys).forEach(function (key) { try { anonymous[keys[key]] = JSON.parse(rawGet(key)) || anonymous[keys[key]]; } catch (e) {} });
        Object.keys(anonymous.pins).forEach(function (key) {
          if (!data.pins[key]) {
            data.pins[key] = anonymous.pins[key];
            if (!data.pins[key].source) data.pins[key].source = 'hn';
          }
        });
        data.domains = Array.from(new Set(data.domains.concat(anonymous.domains))).sort();
        // The account's own entry wins where both have one, as for pins.
        ['progress', 'collections', 'collected', 'notes'].forEach(function (kind) {
          Object.keys(anonymous[kind]).forEach(function (key) { if (!data[kind][key]) data[kind][key] = anonymous[kind][key]; });
        });
        data = rebase(baseline, data, baseline);
        install(data, true); schedule();
        button.textContent = 'Import anonymous data again';
      });
    });
  };
  if (!user) return;

  // Retain edits whose earlier upload failed, including navigation while offline.
  var previous;
  try { previous = JSON.parse(rawGet('yavchn-account:' + user.id + ':baseline')); } catch (e) {}
  var starting = previous ? rebase(previous, documentData(), baseline) : baseline;
  install(starting, false);
  rememberBase();
  if (!same(starting, baseline)) schedule();

  async function fetchData() {
    var response = await fetch(dataURL, { cache: 'no-store', headers: { Accept: 'application/json' } });
    if (response.status === 401) { location.reload(); throw new Error('Your session changed.'); }
    if (!response.ok) throw new Error('Account data is unavailable.');
    var result = await response.json();
    if (result.id !== user.id) { location.reload(); throw new Error('Your account changed.'); }
    return result;
  }
  async function synchronize() {
    if (running) return;
    running = true;
    var before = copy(baseline), submitted = documentData();
    try {
      var remote;
      if (same(before, submitted)) remote = await fetchData();
      else {
        var response = await fetch(user.links.replace.href, {
          method: user.links.replace.method, headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf.content, 'If-Match': '"' + revision + '"' },
          body: JSON.stringify(submitted)
        });
        if (response.status === 401 || response.status === 403) { location.reload(); throw new Error('Your session changed.'); }
        if (response.status === 412) {
          remote = await fetchData();
          var merged = rebase(before, documentData(), remote.data);
          baseline = remote.data; revision = remote.revision;
          install(merged, true); rememberBase(); schedule();
          return;
        }
        if (!response.ok) throw new Error('Changes could not be synchronized.');
        remote = await response.json();
        if (remote.id !== user.id) throw new Error('Your account changed.');
        // Preserve edits made while this upload was in flight.
        before = submitted;
      }
      var current = rebase(before, documentData(), remote.data);
      baseline = remote.data; revision = remote.revision;
      install(current, true); rememberBase();
      status('Your account data is synchronized.');
      if (!same(current, baseline)) schedule();
    } catch (e) {
      status('Sync paused. Your changes remain in this browser and will retry when connected.');
    } finally { running = false; }
  }
  window.addEventListener('online', synchronize);
  setInterval(function () { if (!document.hidden) synchronize(); }, 30000);
  document.addEventListener('visibilitychange', function () { if (!document.hidden) synchronize(); });
  window.addEventListener('storage', function (e) {
    if (!e.key || !e.key.startsWith('yavchn-account:' + user.id + ':')) return;
    Object.keys(keys).forEach(function (key) {
      if (e.key === physical(key)) {
        window.dispatchEvent(new StorageEvent('storage', { key: key, newValue: e.newValue }));
        schedule();
      }
    });
  });

})();
