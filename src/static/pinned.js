(function () {
  var KEY = 'yavchn-pinned';
  var CAP = 500;

  // Storage shape: { "<storyID>": { title, url, host, by, score, comments, pinned_at } }
  function load() {
    try {
      var raw = localStorage.getItem(KEY);
      if (!raw) return {};
      var obj = JSON.parse(raw);
      return (obj && typeof obj === 'object' && !Array.isArray(obj)) ? obj : {};
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
  // after the story rolls off HN's front-page caches.
  function metaOfRow(row) {
    return {
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
    var links = document.querySelectorAll('.source-tabs a');
    for (var i = 0; i < links.length; i++) {
      var a = links[i];
      // Match the Pinned tab by its href ('/pinned/' or '/pinned').
      var href = a.getAttribute('href') || '';
      if (href !== '/pinned/' && href !== '/pinned') continue;
      a.textContent = count > 0 ? ('Pinned (' + count + ')') : 'Pinned';
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
      var title = escapeHTML(s.title || '(no title)');
      var url = escapeAttr(s.url || ('https://news.ycombinator.com/item?id=' + id));
      var host = escapeHTML(s.host || 'news.ycombinator.com');
      var by = escapeHTML(s.by || '');
      var selectURL = escapeAttr('/pinned/s/' + id);
      var pinnedAt = relTime(s.pinned_at || 0);
      html += '<li class="story pinned" data-id="' + escapeAttr(id) + '"' +
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

  // Click handler -- document-level so it survives swap.js and any future
  // list-content replacement.
  document.addEventListener('click', function (e) {
    var btn = e.target.closest('.pane-list .pin-btn');
    if (!btn) return;
    e.preventDefault();
    e.stopPropagation();
    var row = btn.closest('.story');
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
  });

  // Bootstrap: render Pinned tab content first (so applyState sees the
  // rendered rows), then sync .pinned classes + tab badge.
  if (document.querySelector('.pane-list[data-source="pinned"]')) {
    renderPinnedList();
  }
  applyState();
})();
