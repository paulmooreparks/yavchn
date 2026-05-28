(function () {
  var KEY = 'yavchn-pinned';
  var CAP = 500;

  // Storage shape (current): { "<storyID>": { source, title, url, host, by, score, comments, pinned_at } }
  // Legacy entries (pre-multi-source) have no `source` field; load() backfills 'hn'.
  function load() {
    try {
      var raw = localStorage.getItem(KEY);
      if (!raw) return {};
      var obj = JSON.parse(raw);
      if (!obj || typeof obj !== 'object' || Array.isArray(obj)) return {};
      // Backfill missing source -> 'hn' so legacy entries route through the HN tab.
      var migrated = false;
      for (var k in obj) {
        if (!obj[k] || typeof obj[k] !== 'object') continue;
        if (!obj[k].source) { obj[k].source = 'hn'; migrated = true; }
      }
      if (migrated) { try { localStorage.setItem(KEY, JSON.stringify(obj)); } catch (e) {} }
      return obj;
    } catch (e) { return {}; }
  }

  function save(obj) {
    var keys = Object.keys(obj);
    if (keys.length > CAP) {
      // LIFO eviction: drop the oldest pinned_at first.
      keys.sort(function (a, b) {
        return (obj[a].pinned_at || 0) - (obj[b].pinned_at || 0);
      });
      while (keys.length > CAP) {
        delete obj[keys.shift()];
      }
    }
    try { localStorage.setItem(KEY, JSON.stringify(obj)); } catch (e) {}
  }

  function isPinned(id) {
    if (!id) return false;
    return !!load()[String(id)];
  }

  function setPinned(id, meta) {
    if (!id) return;
    var store = load();
    store[String(id)] = Object.assign({}, meta || {}, { pinned_at: Math.floor(Date.now() / 1000) });
    save(store);
  }

  function unpin(id) {
    if (!id) return;
    var store = load();
    delete store[String(id)];
    save(store);
  }

  // Denormalize the row's metadata so the Pinned tab can render it even
  // after the story rolls off the source's caches. Captures `source` so
  // the pinned-list row knows which source to route back to on click.
  function metaOfRow(row) {
    return {
      source: row.dataset.source || 'hn',
      title: (row.querySelector('.title a') || {}).textContent || '',
      url: row.dataset.url || '',
      host: row.dataset.host || '',
      by: row.dataset.by || '',
      score: parseInt(row.dataset.score || '0', 10) || 0,
      comments: parseInt(row.dataset.comments || '0', 10) || 0
    };
  }

  function applyState() {
    var store = load();
    var rows = document.querySelectorAll('.pane-list .story[data-id]');
    for (var i = 0; i < rows.length; i++) {
      var id = rows[i].dataset.id;
      var pinned = !!store[String(id)];
      rows[i].classList.toggle('pinned', pinned);
      var btn = rows[i].querySelector('.pin-btn');
      if (btn) {
        btn.setAttribute('aria-pressed', pinned ? 'true' : 'false');
        btn.setAttribute('title', pinned ? 'Unpin this story' : 'Pin this story');
        btn.setAttribute('aria-label', pinned ? 'Unpin this story' : 'Pin this story');
      }
    }
    updateTabBadge();
  }

  function updateTabBadge() {
    var count = Object.keys(load()).length;
    // Pinned now lives in the source-picker row as a peer of HN/Lobsters.
    var links = document.querySelectorAll('.source-picker a[data-source="pinned"]');
    for (var i = 0; i < links.length; i++) {
      links[i].textContent = count > 0 ? ('Pinned (' + count + ')') : 'Pinned';
    }
  }

  // Render the pinned-stories list into the Pinned tab. Server sends an
  // empty shell; we populate from localStorage so the page works without
  // any per-user state on the server.
  function renderPinnedList() {
    var pane = document.querySelector('.pane-list[data-source="pinned"]');
    if (!pane) return;
    var ul = pane.querySelector('.stories');
    if (!ul) return;
    var store = load();
    var ids = Object.keys(store);
    if (!ids.length) {
      ul.innerHTML = '<li class="empty-pinned"><p class="empty-note">No pinned stories yet. Click the pin icon on any row to save it here.</p></li>';
      return;
    }
    // Newest pin first.
    ids.sort(function (a, b) {
      return (store[b].pinned_at || 0) - (store[a].pinned_at || 0);
    });
    var html = '';
    for (var i = 0; i < ids.length; i++) {
      var id = ids[i];
      var s = store[id];
      var source = s.source || 'hn';
      var title = escapeHTML(s.title || '(no title)');
      var url = escapeAttr(s.url || (source === 'lobsters'
        ? 'https://lobste.rs/s/' + id
        : 'https://news.ycombinator.com/item?id=' + id));
      var defaultHost = source === 'lobsters' ? 'lobste.rs' : 'news.ycombinator.com';
      var host = escapeHTML(s.host || defaultHost);
      var by = escapeHTML(s.by || '');
      // Route back through /pinned/s/{source}/{id} so the article + discussion
      // panes know which source to render.
      var selectURL = escapeAttr('/pinned/s/' + source + '/' + id);
      var pinnedAt = relTime(s.pinned_at || 0);
      html += '<li class="story pinned" data-id="' + escapeAttr(id) + '"' +
        ' data-source="' + escapeAttr(source) + '"' +
        ' data-url="' + url + '" data-host="' + host + '"' +
        ' data-by="' + escapeAttr(by) + '" data-score="' + (s.score || 0) + '"' +
        ' data-comments="' + (s.comments || 0) + '">' +
        '<button type="button" class="pin-btn" title="Unpin this story" aria-label="Unpin this story" aria-pressed="true">' +
        '<svg class="pin-icon" viewBox="0 0 16 16" width="12" height="12" aria-hidden="true">' +
        '<path d="M5 2h6v3l2 3H9v6l-1 1-1-1V8H3l2-3V2z"/>' +
        '</svg></button>' +
        '<span class="rank">' + (i + 1) + '.</span>' +
        '<div class="body">' +
        '<div class="title"><a href="' + selectURL + '">' + title + '</a></div>' +
        '<div class="meta">' +
        '<a class="host" href="' + url + '" target="_blank" rel="noopener">' + host + ' ↑</a>' +
        ' &middot; ' + (s.score || 0) + ' pts' +
        ' &middot; ' + by +
        ' &middot; pinned ' + pinnedAt +
        ' &middot; ' + (s.comments || 0) + ' comments' +
        '</div></div>' +
        '<button type="button" class="dismiss-btn" title="Hide this story" aria-label="Hide this story">×</button>' +
        '</li>';
    }
    ul.innerHTML = html;
    // The newly-rendered rows already carry .pinned; no need to re-apply
    // state, but the badge count may have shifted.
    updateTabBadge();
  }

  function escapeHTML(s) {
    return String(s).replace(/[&<>]/g, function (c) {
      return c === '&' ? '&amp;' : c === '<' ? '&lt;' : '&gt;';
    });
  }
  function escapeAttr(s) {
    return String(s).replace(/[&<>"']/g, function (c) {
      return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c];
    });
  }
  function relTime(unix) {
    if (!unix) return 'just now';
    var d = Math.floor(Date.now() / 1000) - unix;
    if (d < 60) return 'just now';
    if (d < 3600) return Math.floor(d / 60) + 'm ago';
    if (d < 86400) return Math.floor(d / 3600) + 'h ago';
    return Math.floor(d / 86400) + 'd ago';
  }

  function togglePin(row) {
    if (!row || !row.dataset.id) return;
    var id = row.dataset.id;
    if (isPinned(id)) {
      unpin(id);
      // On the Pinned tab, unpinning removes the row entirely.
      if (document.querySelector('.pane-list[data-source="pinned"]')) {
        row.remove();
        renderPinnedList();
        return;
      }
    } else {
      setPinned(id, metaOfRow(row));
    }
    applyState();
  }

  function inEditable(t) {
    if (!t) return false;
    var tag = (t.tagName || '').toLowerCase();
    if (tag === 'input' || tag === 'textarea' || tag === 'select') return true;
    return !!t.isContentEditable;
  }

  // Click handler -- document-level so it survives swap.js and any future
  // list-content replacement.
  document.addEventListener('click', function (e) {
    var btn = e.target.closest('.pane-list .pin-btn');
    if (!btn) return;
    e.preventDefault();
    e.stopPropagation();
    togglePin(btn.closest('.story'));
  });

  // Keyboard shortcut: 'p' toggles pin on the currently-focused row
  // (keys.js maintains the .focused class via j/k navigation).
  document.addEventListener('keydown', function (e) {
    if (e.ctrlKey || e.altKey || e.metaKey || e.shiftKey) return;
    if (e.key !== 'p') return;
    if (inEditable(e.target)) return;
    var focused = document.querySelector('.pane-list .story.focused');
    if (!focused) return;
    e.preventDefault();
    togglePin(focused);
  });

  // Bootstrap: render Pinned tab content first (so applyState sees the
  // rendered rows), then sync .pinned classes + tab badge.
  if (document.querySelector('.pane-list[data-source="pinned"]')) {
    renderPinnedList();
  }
  applyState();
})();
