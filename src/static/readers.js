/* Reader instances own their article selection; PUDL owns their windows. */
(function () {
  'use strict';
  var lib = window.yavchn;
  var mounted = new Map();
  var pending = new Map();
  var recent = [];
  var sites = {
    hn: { label: 'HN', name: 'Hacker News', icon: '/static/hn-favicon.svg' },
    lobsters: { label: 'Lobsters', name: 'Lobsters', icon: '/static/lobsters-favicon.png' }
  };

  // The site's mark in a reader's title bar: its icon, with its name for
  // assistive technology, which hears it before the title.
  function badge(site) {
    var span = document.createElement('span');
    span.className = 'reader-site';
    var img = document.createElement('img');
    img.className = 'source-icon';
    img.src = site.icon;
    img.alt = '';
    img.width = img.height = 16;
    var label = document.createElement('span');
    label.textContent = site.label;
    label.className = 'visually-hidden';
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
        var mark = badge(site);
        mark.id = 'win-' + key + '-site';
        head.insertBefore(mark, title);
        win.setAttribute('aria-labelledby', mark.id + ' ' + title.id);
      } else win.setAttribute('aria-labelledby', title.id);
      /* PUDL draws the site's picture on the reader's taskbar button, its
         dock tab and every list of windows (0.96.3), and stacks the readers
         of one site when the taskbar runs short (0.95.0). The server writes
         these for the first page; a reader that changes site follows here. */
      setAttr(win, 'data-win-icon', site && site.icon);
      setAttr(win, 'data-win-group', site && root.dataset.storySource);
      setAttr(win, 'data-win-group-label', site && site.name);
    });
  }

  // Sets or clears an attribute only when it changes, since PUDL redraws
  // the window's taskbar button on each change.
  function setAttr(el, name, value) {
    if (value) { if (el.getAttribute(name) !== value) el.setAttribute(name, value); }
    else if (el.hasAttribute(name)) el.removeAttribute(name);
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
