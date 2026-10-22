/* The story list's context menu: a right-click on a row, or Shift+F10 or
   the Menu key on the row that has focus, opens PUDL's context menu of
   what can be done with that story. PUDL marks the row while the menu is
   open (detail.mark), so the reader sees which story the commands act on.

   Each command is one the list already has, done the same way: the pin,
   hide and remove-from-collection rows press the row's own buttons, the
   collections come from library.js, and opening goes through readers.js,
   so the menu adds no behaviour the row does not have. */
(function () {
  'use strict';
  var lib = window.yavchn;

  // Each source's own page for a story, as hn.go and lobsters.go write it.
  var SOURCES = {
    hn: { label: 'Hacker News', page: function (id) { return 'https://news.ycombinator.com/item?id=' + encodeURIComponent(id); } },
    lobsters: { label: 'Lobsters', page: function (id) { return 'https://lobste.rs/s/' + encodeURIComponent(id); } }
  };

  // Shared with the comments' menu in discussion.js.
  function copy(text, what) {
    what = what || 'link';
    var done = function () { if (window.pudlToast) window.pudlToast(what.charAt(0).toUpperCase() + what.slice(1) + ' copied.', { kind: 'positive' }); };
    if (navigator.clipboard) navigator.clipboard.writeText(text).then(done, function () {
      if (window.pudlToast) window.pudlToast('The ' + what + ' could not be copied.', { kind: 'warn' });
    });
  }
  lib.copy = copy;
  function press(row, selector) { var b = row.querySelector(selector); if (b) b.click(); }

  function rowsFor(row) {
    var d = row.dataset;
    var link = row.querySelector(':scope > a.md-item');
    var key = d.key || (link && link.getAttribute('data-win-open'));
    var source = SOURCES[d.source];
    var page = link ? new URL(link.getAttribute('href'), location.href).href : '';
    var url = d.url || '';
    // A text post's address is its source's own page, which is no article.
    var original = url && !(source && url === source.page(d.id)) && !/^https:\/\/(news\.ycombinator\.com\/item|lobste\.rs\/s\/)/.test(url) ? url : '';
    var windows = !!(document.querySelector('[data-win-layer]') && lib.readers);
    var pinned = !!(lib.pins && lib.pins.isPinned(d.id));
    var read = !!(lib.visited && lib.visited.has(d.id));
    var rows = [
      { label: windows ? 'Open in the reader' : 'Open', glyph: 'document', run: function () { if (link) link.click(); } }
    ];
    if (windows && key) rows.push({ label: 'Open in a new reader window', glyph: 'new-window', run: function () { lib.readers.create(key); } });
    if (page) rows.push({ label: 'Open in a new tab', run: function () { window.open(page, '_blank', 'noopener'); } });
    rows.push('-');
    if (original) rows.push({ label: 'Open the original', glyph: 'open', run: function () { window.open(original, '_blank', 'noopener'); } });
    if (source) rows.push({ label: 'Open on ' + source.label, glyph: 'link', run: function () { window.open(source.page(d.id), '_blank', 'noopener'); } });
    rows.push('-');
    if (row.querySelector('.story-pin')) rows.push({ label: 'Pin this story', glyph: 'pin', checked: pinned, run: function () { press(row, '.story-pin'); } });
    if (lib.library) {
      var collections = lib.library.storyMenu(row)[0];
      if (collections) rows.push(collections);
    }
    if (lib.visited) rows.push({ label: read ? 'Mark as unread' : 'Mark as read', run: function () { lib.visited.set(d.id, !read); } });
    rows.push('-');
    if (page) rows.push({ label: 'Copy link to the story', glyph: 'copy', run: function () { copy(page); } });
    if (original) rows.push({ label: 'Copy link to the article', glyph: 'copy', run: function () { copy(original); } });
    rows.push('-');
    var uncollect = row.querySelector('.story-uncollect');
    if (uncollect) rows.push({ label: uncollect.getAttribute('aria-label'), glyph: 'close', run: function () { uncollect.click(); } });
    if (row.querySelector('.story-hide')) rows.push({ label: 'Hide this story', glyph: 'close', run: function () { press(row, '.story-hide'); } });
    if (original && d.host && lib.hiding && lib.hiding.blockDomain) {
      rows.push({ label: 'Block ' + d.host + ' across all feeds', danger: true, run: function () { lib.hiding.blockDomain(d.host); } });
    }
    return rows;
  }

  document.addEventListener('pudl:contextmenu', function (e) {
    var target = e.detail && e.detail.target;
    var row = target && target.closest && target.closest('.story-list .story-row[data-id]');
    if (!row) return;
    e.detail.mark(row);
    e.detail.add(rowsFor(row));
  });
})();
