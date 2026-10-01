/* Pinned stories, kept in this browser under yavchn-pinned and never sent
   to the server. A row's pin, the p key on the marked row, and a story
   window's menu pin and unpin; the Pinned view's list is rendered here
   from the store, since the server keeps no per-reader state. */
(function () {
  'use strict';
  var KEY = 'yavchn-pinned';
  var CAP = 500;

  // Storage shape: { "<storyID>": { source, title, url, host, by, score, comments, pinned_at } }
  // Legacy entries (pre-multi-source) have no `source` field; load() backfills 'hn'.
  function load() {
    try {
      var obj = JSON.parse(localStorage.getItem(KEY) || '{}');
      if (!obj || typeof obj !== 'object' || Array.isArray(obj)) return {};
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
      // Drop the oldest pins first.
      keys.sort(function (a, b) { return (obj[a].pinned_at || 0) - (obj[b].pinned_at || 0); });
      while (keys.length > CAP) delete obj[keys.shift()];
    }
    try { localStorage.setItem(KEY, JSON.stringify(obj)); } catch (e) {}
  }

  function isPinned(id) { return !!id && !!load()[String(id)]; }

  function setPinned(id, meta, on) {
    if (!id) return;
    var store = load();
    if (on) store[String(id)] = Object.assign({}, meta, { pinned_at: Math.floor(Date.now() / 1000) });
    else delete store[String(id)];
    save(store);
  }

  // The row's or the story's metadata goes into the store, so Pinned can
  // list the story after it has left its source's lists.
  function metaOf(el, title) {
    var d = el.dataset;
    return {
      source: d.source || d.storySource || 'hn',
      title: title || '',
      url: d.url || '',
      host: d.host || '',
      by: d.by || '',
      score: parseInt(d.score || '0', 10) || 0,
      comments: parseInt(d.comments || '0', 10) || 0
    };
  }

  function pinnedView() { return document.querySelector('.story-list[data-source="pinned"]'); }

  function applyState() {
    var store = load();
    document.querySelectorAll('.story-row[data-id]').forEach(function (row) {
      var pinned = !!store[row.dataset.id];
      row.classList.toggle('pinned', pinned);
      var btn = row.querySelector('.story-pin');
      if (btn) {
        btn.setAttribute('aria-pressed', pinned ? 'true' : 'false');
        btn.setAttribute('aria-label', pinned ? 'Unpin this story' : 'Pin this story');
      }
    });
    var count = Object.keys(store).length;
    document.querySelectorAll('.source-seg a[data-source="pinned"]').forEach(function (a) {
      a.textContent = count > 0 ? 'Pinned (' + count + ')' : 'Pinned';
    });
  }

  function esc(s) {
    return String(s).replace(/[&<>"']/g, function (c) {
      return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c];
    });
  }

  function relTime(unix) {
    var d = Math.floor(Date.now() / 1000) - (unix || 0);
    if (!unix || d < 60) return 'just now';
    if (d < 3600) return Math.floor(d / 60) + 'm ago';
    if (d < 86400) return Math.floor(d / 3600) + 'h ago';
    if (d < 30 * 86400) return Math.floor(d / 86400) + 'd ago';
    var t = new Date(unix * 1000);
    var pad = function (n) { return n < 10 ? '0' + n : '' + n; };
    return t.getFullYear() + '-' + pad(t.getMonth() + 1) + '-' + pad(t.getDate());
  }

  /* The words filter: every word must appear in the title, the site or the
     author; a "quoted phrase" is taken whole, and a -word leaves out the
     stories that have it, as in the hiring filter. */
  function parseTerms(q) {
    var want = [], not = [];
    (String(q || '').toLowerCase().match(/-?"[^"]*"|\S+/g) || []).forEach(function (tok) {
      var neg = tok.charAt(0) === '-' && tok.length > 1;
      tok = tok.replace(/^-/, '').replace(/^"|"$/g, '');
      if (tok) (neg ? not : want).push(tok);
    });
    return { want: want, not: not };
  }

  /* The filters and order come from the list's attributes, which the
     server renders from the address, so a filtered Pinned view is an
     address like any other. The words box overrides the words as the
     reader types, before Enter puts them in the address. */
  function filtersOf(view) {
    var input = document.querySelector('.pin-filter input[name="q"]');
    return {
      terms: parseTerms(input ? input.value : view.dataset.pinQ),
      source: view.dataset.pinSource || '',
      unread: view.hasAttribute('data-pin-unread'),
      sort: view.dataset.pinSort || ''
    };
  }

  var ORDERS = {
    '': function (a, b) { return (b.pinned_at || 0) - (a.pinned_at || 0); },
    oldest: function (a, b) { return (a.pinned_at || 0) - (b.pinned_at || 0); },
    points: function (a, b) { return (b.score || 0) - (a.score || 0); },
    comments: function (a, b) { return (b.comments || 0) - (a.comments || 0); }
  };

  /* The Pinned view's rows, the same markup the server renders for every
     other list, filtered and ordered as the address says. */
  function renderPinnedList() {
    var view = pinnedView();
    var box = view && view.querySelector('.stories');
    if (!box) return;
    var store = load();
    var all = Object.keys(store);
    if (!all.length) {
      box.innerHTML = '<div class="empty-state list-empty"><p class="empty-state-title">No pinned stories</p>' +
        '<p class="empty-state-body">The pin at the start of a story\'s row, or the p key, keeps the story here.</p></div>';
      return;
    }
    var f = filtersOf(view);
    var visited = window.yavchn.visited;
    var ids = all.filter(function (id) {
      var s = store[id];
      if (f.source && (s.source || 'hn') !== f.source) return false;
      if (f.unread && visited && visited.has(id)) return false;
      var text = ((s.title || '') + ' ' + (s.host || '') + ' ' + (s.by || '')).toLowerCase();
      return f.terms.want.every(function (w) { return text.indexOf(w) >= 0; }) &&
        !f.terms.not.some(function (n) { return text.indexOf(n) >= 0; });
    }).sort(function (a, b) { return (ORDERS[f.sort] || ORDERS[''])(store[a], store[b]); });

    var filtered = ids.length !== all.length;
    var count = filtered ? '<p class="pin-count num">' + ids.length + ' of ' + all.length + ' pinned stories</p>' : '';
    if (!ids.length) {
      box.innerHTML = count + '<div class="empty-state list-empty"><p class="empty-state-title">No pinned story matches</p>' +
        '<p class="empty-state-body">None of your ' + all.length + ' pinned stories passes these filters.</p></div>';
      box.dispatchEvent(new CustomEvent('yavchn:rows-appended', { bubbles: true }));
      return;
    }
    var front = window.pudlWindows ? window.pudlWindows.state() : null;
    box.innerHTML = count + ids.map(function (id) {
      var s = store[id];
      var source = s.source || 'hn';
      var key = source + '-' + id;
      var current = front && front.top === key && !front.min[key];
      var host = s.host || (source === 'lobsters' ? 'lobste.rs' : 'news.ycombinator.com');
      return '<div class="md-row story-row pinned' + (current ? ' active' : '') + '" id="row-' + esc(key) + '" data-key="' + esc(key) + '"' +
        ' data-id="' + esc(id) + '" data-source="' + esc(source) + '" data-url="' + esc(s.url || '') + '"' +
        ' data-host="' + esc(host) + '" data-by="' + esc(s.by || '') + '" data-score="' + (s.score || 0) + '"' +
        ' data-comments="' + (s.comments || 0) + '">' +
        '<button type="button" class="icon-btn story-pin" aria-pressed="true" aria-label="Unpin this story"></button>' +
        '<a class="md-item" href="/story/' + esc(source) + '/' + esc(id) + '" data-win-open="' + esc(key) + '"' +
        (current ? ' aria-current="true"' : '') + '>' + esc(s.title || '(no title)') +
        '<span class="md-meta num">' + esc(host) + ' &middot; ' + (s.score || 0) + ' points' +
        (s.by ? ' &middot; ' + esc(s.by) : '') + ' &middot; pinned ' + relTime(s.pinned_at) +
        ' &middot; ' + (s.comments || 0) + ' comments</span></a>' +
        '<button type="button" class="icon-btn story-hide" aria-label="Hide this story"></button></div>';
    }).join('');
    box.dispatchEvent(new CustomEvent('yavchn:rows-appended', { bubbles: true }));
  }

  function toggleRow(row) {
    if (!row || !row.dataset.id) return;
    var id = row.dataset.id;
    if (isPinned(id)) {
      setPinned(id, null, false);
      // In Pinned, unpinning takes the row away.
      if (pinnedView() && pinnedView().contains(row)) { renderPinnedList(); }
    } else {
      var link = row.querySelector('.md-item');
      setPinned(id, metaOf(row, link ? link.firstChild.textContent.trim() : ''), true);
    }
    applyState();
  }

  function toggleStory(story) {
    var id = story.dataset.storyId;
    setPinned(id, metaOf(story, story.dataset.title), !isPinned(id));
    if (pinnedView()) renderPinnedList();
    applyState();
  }

  window.yavchn.pins = { isPinned: isPinned, toggleStory: toggleStory };

  document.addEventListener('click', function (e) {
    var btn = e.target.closest && e.target.closest('.story-row .story-pin');
    if (!btn) return;
    e.preventDefault();
    toggleRow(btn.closest('.story-row'));
  });

  document.addEventListener('keydown', function (e) {
    if (e.key !== 'p' || e.shiftKey || !window.yavchn.plainKey(e)) return;
    var row = document.querySelector('.story-list .story-row.focused');
    if (!row) return;
    e.preventDefault();
    toggleRow(row);
  });

  // The words filter narrows the list as the reader types.
  var typing = 0;
  document.addEventListener('input', function (e) {
    if (!e.target.matches || !e.target.matches('.pin-filter input[name="q"]')) return;
    clearTimeout(typing);
    typing = setTimeout(renderPinnedList, 120);
  });

  // A pin made in another tab shows here too.
  window.addEventListener('storage', function (e) {
    if (e.key !== KEY) return;
    if (pinnedView()) renderPinnedList();
    applyState();
  });

  window.yavchn.onList(function () {
    if (pinnedView()) renderPinnedList();
    applyState();
  });
  document.addEventListener('yavchn:rows-appended', applyState);
})();
