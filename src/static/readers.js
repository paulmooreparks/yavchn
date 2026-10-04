/* Reader instances own their article selection; PUDL owns their windows. */
(function () {
  'use strict';
  var lib = window.yavchn;
  var mounted = new Map();
  var pending = new Map();
  var recent = [];
  var sites = {
    hn: { label: 'HN', icon: '/static/hn-favicon.svg' },
    lobsters: { label: 'Lobsters', icon: '/static/lobsters-favicon.png' }
  };

  function badge(site, iconOnly) {
    var span = document.createElement('span');
    span.className = 'reader-site';
    var img = document.createElement('img');
    img.className = 'source-icon';
    img.src = site.icon;
    img.alt = '';
    img.width = img.height = 16;
    var label = document.createElement('span');
    label.textContent = site.label;
    if (iconOnly) label.className = 'visually-hidden';
    span.append(img, label);
    span.title = site.label;
    return span;
  }

  function syncChrome() {
    document.querySelectorAll('.win.story-win').forEach(function (win) {
      var key = win.getAttribute('data-win');
      var root = win.querySelector('.story');
      var site = root && sites[root.dataset.storySource];
      var head = win.querySelector('.win-head');
      var old = head.querySelector('.reader-site');
      if (old) old.remove();
      var title = win.querySelector('.win-title');
      if (site) {
        var mark = badge(site, true);
        mark.id = 'win-' + key + '-site';
        head.insertBefore(mark, title);
        win.setAttribute('aria-labelledby', mark.id + ' ' + title.id);
      } else win.setAttribute('aria-labelledby', title.id);
      document.querySelectorAll('[data-win-tab="' + CSS.escape(key) + '"]').forEach(function (tab) {
        var headline = document.createElement('span');
        headline.className = 'reader-tab-title';
        headline.textContent = title.textContent.trim();
        tab.replaceChildren(headline);
        if (site) {
          tab.prepend(badge(site));
          tab.setAttribute('data-reader-source', root.dataset.storySource);
          tab.title = site.label + ': ' + title.textContent.trim() + (tab.classList.contains('minimized') ? ' (minimized)' : '');
        } else {
          tab.removeAttribute('data-reader-source');
          tab.title = headline.textContent + (tab.classList.contains('minimized') ? ' (minimized)' : '');
        }
      });
    });
  }

  // Applet icons come from host-owned window metadata, with PUDL's app glyph as fallback.
  function syncAppletIcons() {
    document.querySelectorAll('.win.app-win').forEach(function (win) {
      var key = win.getAttribute('data-win');
      document.querySelectorAll('[data-win-tab="' + CSS.escape(key) + '"]').forEach(function (tab) {
        var title = document.createElement('span');
        title.className = 'task-title';
        title.textContent = win.querySelector('.win-title').textContent.trim();
        var iconURL = win.getAttribute('data-window-icon');
        var icon = document.createElement(iconURL ? 'img' : 'span');
        icon.className = 'task-icon';
        if (iconURL) { icon.src = iconURL; icon.alt = ''; }
        else {
          icon.classList.add('glyph');
          icon.style.setProperty('--glyph', 'var(--glyph-' + (win.getAttribute('data-window-glyph') || 'app') + ')');
        }
        icon.setAttribute('aria-hidden', 'true');
        tab.replaceChildren(icon, title);
      });
    });
  }

  function remember(key) {
    recent = recent.filter(function (k) { return k !== key; });
    recent.push(key);
  }

  function selected(key) {
    var q = new URLSearchParams(location.search);
    return q.get('r.' + key) || (/^reader-[1-9][0-9]*$/.test(key) || key === 'story' ? '' : key);
  }

  function assign(key, article, push) {
    var url = new URL(location.href);
    if (article) url.searchParams.set('r.' + key, article);
    else url.searchParams.delete('r.' + key);
    if (url.href !== location.href) history[push ? 'pushState' : 'replaceState'](history.state, '', url);
  }

  function newHref(article) {
    var url = new URL(location.href);
    var open = window.pudlWindows ? window.pudlWindows.state().open.slice() : [];
    var n = 1;
    while (open.indexOf('reader-' + n) >= 0 || pending.has('reader-' + n)) n++;
    var key = 'reader-' + n;
    open.push(key);
    url.searchParams.set('open', open.join(','));
    url.searchParams.set('top', key);
    url.searchParams.delete('r.' + key);
    if (article) url.searchParams.set('r.' + key, article);
    return url.pathname + url.search + url.hash;
  }

  function create(article, from) {
    if (!window.pudlWindows) return false;
    var key = new URL(newHref(article), location.href).searchParams.get('top');
    pending.set(key, article || '');
    window.pudlWindows.open(key, from || null);
    return true;
  }

  function read(article, from, ownWindow) {
    if (!window.pudlWindows) return false;
    var source = article.split('-')[0];
    if (ownWindow) {
      var own = mounted.get(ownWindow.getAttribute('data-win'));
      if (own) { own.source = source; own.load(article, true); return true; }
      return false;
    }
    var st = window.pudlWindows.state();
    var candidates = recent.concat(st.open).filter(function (key) {
      var entry = mounted.get(key);
      return entry && entry.root.isConnected &&
        (!entry.source || entry.source === source) &&
        entry.root.closest('.win').getAttribute('data-win-mode').indexOf('dock-') !== 0;
    });
    // Prefer a visible reader, but restore a minimized one before creating
    // another. Mobile's Stories action minimizes readers to reveal the list.
    var visible = candidates.filter(function (key) { return !st.min[key]; });
    if (visible.length) candidates = visible;
    // Activity wins over opening order, including a reader behind another applet.
    var key = recent.slice().reverse().find(function (k) { return candidates.indexOf(k) >= 0; }) || candidates[candidates.length - 1];
    if (!key) return create(article, from);
    mounted.get(key).source = source;
    mounted.get(key).load(article, true);
    return true;
  }

  function syncRows() {
    syncChrome();
    syncAppletIcons();
    var front = document.querySelector('.win.active:not([hidden]) .story');
    document.querySelectorAll('.story-list .story-row').forEach(function (row) {
      var link = row.querySelector('a[data-win-open]');
      var current = !!(front && row.dataset.key === front.dataset.storyKey);
      row.classList.toggle('active', current);
      if (link) {
        if (current) link.setAttribute('aria-current', 'true');
        else link.removeAttribute('aria-current');
      }
    });
    document.querySelectorAll('a[data-reader-new]').forEach(function (a) {
      a.href = newHref(a.getAttribute('data-reader-new'));
    });
  }

  lib.readers = {
    read: read, create: create, assign: assign,
    mount: function (root, load) {
      var win = root.closest('.win[data-win]');
      if (!win) return function () {};
      var key = win.getAttribute('data-win');
      root.setAttribute('data-state-key', key + (root.dataset.storyKey ? ':' + root.dataset.storyKey : ''));
      if (win.classList.contains('active')) remember(key);
      var article = pending.has(key) ? pending.get(key) : selected(key);
      mounted.set(key, { root: root, load: load, source: article ? article.split('-')[0] : '' });
      var fresh = pending.has(key);
      pending.delete(key);
      if (fresh || article !== (root.dataset.storyKey || '')) load(article, false);
      syncRows();
      return function () { mounted.delete(key); };
    }
  };

  document.addEventListener('click', function (e) {
    var a = e.target.closest && e.target.closest('a[data-reader-new]');
    if (a && lib.plainClick(e) && create(a.getAttribute('data-reader-new'), a)) e.preventDefault();
  }, true);
  document.addEventListener('pudl:windows-change', function () {
    var front = document.querySelector('.win.active:not([hidden])');
    if (front && front.querySelector('.story')) remember(front.getAttribute('data-win'));
    syncRows();
  });
  document.addEventListener('yavchn:story-change', function (e) {
    var root = e.target.closest('.story');
    var win = root && root.closest('.win');
    var entry = win && mounted.get(win.getAttribute('data-win'));
    if (entry) entry.source = root.dataset.storySource || '';
    syncRows();
  });
  document.addEventListener('yavchn:rows-appended', syncRows);
  lib.onList(syncRows);
  // PUDL also rebuilds dock tabs for placement policies and title changes.
  // Observe the dock's direct children so decorating a tab does not retrigger us.
  var taskDock = document.querySelector('[data-win-dock]');
  if (taskDock) new MutationObserver(function () {
    syncChrome();
    syncAppletIcons();
  }).observe(taskDock, { childList: true });
  window.addEventListener('popstate', function () {
    mounted.forEach(function (entry, key) {
      if (entry.root.isConnected) {
        var article = selected(key);
        entry.source = article ? article.split('-')[0] : '';
        entry.load(article, false);
      }
    });
  });
})();

/* Keep a visible route to taskbar entries even when scrollbars auto-hide. */
(function () {
  var dock = document.querySelector('[data-win-dock]');
  if (!dock) return;
  var buttons = document.querySelectorAll('[data-taskbar-scroll]');
  function sync() {
    var overflow = dock.scrollWidth > dock.clientWidth + 2;
    buttons.forEach(function (b) {
      b.hidden = !overflow;
      b.disabled = b.dataset.taskbarScroll === '-1' ? dock.scrollLeft <= 1 : dock.scrollLeft + dock.clientWidth >= dock.scrollWidth - 1;
    });
  }
  buttons.forEach(function (b) { b.addEventListener('click', function () { dock.scrollBy({ left: Number(b.dataset.taskbarScroll) * dock.clientWidth * 0.8, behavior: 'smooth' }); }); });
  dock.addEventListener('scroll', sync, { passive: true });
  new ResizeObserver(sync).observe(dock);
  new MutationObserver(sync).observe(dock, { childList: true, subtree: true });
  sync();
})();
