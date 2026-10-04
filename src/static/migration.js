/* Browser storage moves between origins through an explicit local file. */
(function () {
  'use strict';
  // Backups are saved only on the old domain and imported everywhere else.
  var legacy = location.hostname === 'yavchn.parkscomputing.com';
  var limit = 10 * 1024 * 1024;
  var pending = new WeakMap();

  function status(root, message) {
    root.querySelector('[data-migration-status]').textContent = message;
  }
  function validate(text) {
    var backup = JSON.parse(text);
    if (!backup || backup.format !== 'yavchn-browser-storage' || backup.version !== 1 ||
        !backup.storage || typeof backup.storage !== 'object' || Array.isArray(backup.storage) ||
        !Object.values(backup.storage).every(function (value) { return typeof value === 'string'; })) {
      throw new Error('Choose a valid YAVCHN browser data backup.');
    }
    return backup.storage;
  }
  function init() {
    document.querySelectorAll('[data-migration]').forEach(function (root) {
      if (root.dataset.migrationReady) return;
      root.dataset.migrationReady = 'true';
      root.querySelector('[data-migration-export]').disabled = false;
      root.querySelector('[data-migration-leaving]').hidden = !legacy;
      root.querySelector('[data-migration-import]').hidden = legacy;
      root.querySelector('[data-migration-file]').disabled = false;
    });
  }
  document.addEventListener('click', function (event) {
    var button = event.target.closest('[data-migration-export], [data-migration-apply]');
    if (!button) return;
    var root = button.closest('[data-migration]');
    if (button.hasAttribute('data-migration-export')) {
      try {
        var storage = Object.create(null);
        for (var i = 0; i < localStorage.length; i++) {
          var key = localStorage.key(i);
          storage[key] = localStorage.getItem(key);
        }
        var backup = { format: 'yavchn-browser-storage', version: 1,
          origin: location.origin, exportedAt: new Date().toISOString(), storage: storage };
        var url = URL.createObjectURL(new Blob([JSON.stringify(backup, null, 2)], { type: 'application/json' }));
        var link = document.createElement('a');
        link.href = url;
        link.download = 'yavchn-browser-data-' + new Date().toISOString().slice(0, 10) + '.json';
        document.body.appendChild(link);
        link.click();
        link.remove();
        setTimeout(function () { URL.revokeObjectURL(url); }, 1000);
        status(root, 'Backup download started. Keep the file, then continue to yavchn.com to import it.');
      } catch (err) { status(root, 'Browser data could not be saved. Check that browser storage and downloads are allowed, then try again.'); }
      return;
    }
    var entries = pending.get(root);
    if (!entries) return;
    var previous = new Map();
    try {
      Object.keys(entries).forEach(function (key) { previous.set(key, localStorage.getItem(key)); });
      Object.keys(entries).forEach(function (key) { localStorage.setItem(key, entries[key]); });
    } catch (err) {
      var restored = true;
      previous.forEach(function (value, key) {
        try {
          if (value === null) localStorage.removeItem(key);
          else localStorage.setItem(key, value);
        } catch (rollbackError) { restored = false; }
      });
      status(root, restored ? 'Import failed. Your previous data was restored. Check available browser storage and try again.' :
        'Import failed and some entries could not be restored. Keep your backup and retry when browser storage is available.');
      return;
    }
    // Reload immediately so mounted applets cannot save stale state over the import.
    location.assign('/hn/?migration=complete');
  });
  document.addEventListener('change', async function (event) {
    if (!event.target.matches('[data-migration-file]')) return;
    var input = event.target;
    var root = input.closest('[data-migration]');
    var button = root.querySelector('[data-migration-apply]');
    var preview = root.querySelector('[data-migration-preview]');
    var file = input.files[0];
    pending.delete(root);
    button.disabled = true;
    preview.textContent = '';
    status(root, '');
    if (!file) return;
    try {
      if (file.size > limit) throw new Error('The backup is too large. Choose a file smaller than 10 MB.');
      var entries = validate(await file.text());
      if (input.files[0] !== file) return;
      var keys = Object.keys(entries);
      if (!keys.length) throw new Error('This backup contains no browser data to import.');
      var matching = keys.filter(function (key) { return localStorage.getItem(key) !== null; }).length;
      preview.textContent = keys.length + ' storage entries will be imported; ' + matching + ' existing entries will be replaced.';
      pending.set(root, entries);
      button.disabled = false;
    } catch (err) {
      if (input.files[0] === file) status(root, err instanceof SyntaxError ? 'This file is not valid JSON. Choose a YAVCHN backup.' : err.message);
    }
  });
  document.addEventListener('pudl:window-open', init);
  init();
  if (legacy && window.pudlToast) {
    var toast = window.pudlToast('YAVCHN is now available on yavchn.com.', { sticky: true });
    var action = document.createElement('a');
    action.className = 'btn primary migration-toast-action';
    action.href = '/settings?migration=1#browser-data';
    action.textContent = 'Start migration';
    toast.querySelector('.toast-text').appendChild(action);
  }
  if (new URLSearchParams(location.search).get('migration') === 'complete' && window.pudlToast) {
    window.pudlToast('Browser data imported.', { kind: 'positive' });
    var clean = new URL(location.href);
    clean.searchParams.delete('migration');
    history.replaceState(history.state, '', clean.pathname + clean.search + clean.hash);
  }
})();
