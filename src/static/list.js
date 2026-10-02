/* The keyboard's place in the story list: j and k move it, Enter opens the
   story there in its window, and a click on a row moves it there too, so
   the mark always follows the reader's last move. The arrow keys are left
   to scroll whatever has focus. Rows that infinite scroll appends, and a
   whole new list from a region swap, take part as they arrive. */
(function () {
  'use strict';

  function rows() {
    return Array.prototype.slice.call(document.querySelectorAll('.story-list .story-row'))
      .filter(function (r) { return r.offsetParent !== null; });
  }

  function focused() { return document.querySelector('.story-list .story-row.focused'); }

  function mark(row, scroll) {
    document.querySelectorAll('.story-list .story-row.focused').forEach(function (r) {
      if (r !== row) r.classList.remove('focused');
    });
    if (!row) return;
    row.classList.add('focused');
    if (scroll) row.scrollIntoView({ block: 'nearest' });
  }

  /* A new list starts at the story whose window is in front, if it is in
     the list, and otherwise at the top. */
  function start() {
    var current = document.querySelector('.story-list .story-row:has(> [aria-current]:not([aria-current="false"]))');
    mark(current || rows()[0], false);
  }
  window.yavchn.onList(start);
  // Rows rendered by script, as Pinned's are, arrive after the list.
  document.addEventListener('yavchn:rows-appended', function () { if (!focused()) start(); });

  /* A plain story choice goes to an available reader for its source. The applet changes its own story while its window and instance
     stay alive. The first choice still opens the reader normally. */
  document.addEventListener('click', function (e) {
    if (!window.yavchn.plainClick(e) || !window.yavchn.readers) return;
    var link = e.target.closest && e.target.closest('.story-list a[data-win-open], .story-next[data-win-open]');
    if (!link) return;
    if (link.matches('.story-next')) syncNext(link);
    var key = link.getAttribute('data-win-open');
    if (!key || !window.yavchn.readers.read(key, link, link.matches('.story-next') ? link.closest('.win') : null)) return;
    e.preventDefault();
  }, true);

  document.addEventListener('click', function (e) {
    if (e.button !== 0) return;
    var row = e.target.closest('.story-list .story-row');
    if (row && !e.target.closest('.story-pin, .story-hide')) mark(row, false);
  });

  /* The mark follows the window in front, so after Next, or a click on a
     dock tab, j and k carry on from the story being read. */
  document.addEventListener('pudl:windows-change', function () {
    var current = document.querySelector('.story-list .story-row:has(> [aria-current]:not([aria-current="false"]))');
    if (current && current !== focused()) mark(current, true);
  });

  /* === Next story =======================================================
     A story window's Next link loads the story after it into the running
     applet, so the reader can go down a list in one instance, and Back
     steps back up it. The list decides what is next: the following row
     that is not hidden or filtered out, even with the list itself hidden. */
  function nextLink(key) {
    var list = Array.prototype.slice.call(document.querySelectorAll('.story-list .story-row:not(.dismissed):not(.filtered)'));
    var i = list.findIndex(function (r) { return r.dataset.key === key; });
    return i >= 0 && list[i + 1] ? list[i + 1].querySelector('a[data-win-open]') : null;
  }

  function syncNext(a) {
    var story = a.closest('.story');
    var next = story && a.closest('.win') ? nextLink(story.dataset.storyKey) : null;
    a.hidden = !a.closest('.win');
    a.setAttribute('aria-disabled', String(!next));
    if (!next) { a.removeAttribute('data-win-open'); a.removeAttribute('href'); a.setAttribute('aria-label', 'Next story'); return; }
    a.setAttribute('data-win-open', next.getAttribute('data-win-open'));
    a.setAttribute('href', next.getAttribute('href'));
    a.setAttribute('aria-label', 'Next story: ' + next.firstChild.textContent.trim());
  }

  function syncAllNext() { document.querySelectorAll('.story-next').forEach(syncNext); }
  window.yavchn.onList(syncAllNext);
  ['yavchn:rows-appended', 'yavchn:list-change', 'yavchn:story-change', 'pudl:window-open', 'pudl:windows-change'].forEach(function (name) {
    document.addEventListener(name, syncAllNext);
  });
  /* Rows hide and pins change between those events, so the link is set
     again just before pudl-windows.js reads it. */
  document.addEventListener('click', function (e) {
    var a = e.target.closest && e.target.closest('.story-next');
    if (a) syncNext(a);
    else if (e.target.closest && e.target.closest('.story-hide, .story-show-hidden')) setTimeout(syncAllNext, 0);
  }, true);

  /* The Next link of a window, current, or null when nothing follows it. */
  function nextStory(win) {
    var a = win && win.querySelector('.story-next');
    if (a) syncNext(a);
    return a && !a.hidden && a.getAttribute('aria-disabled') !== 'true' ? a : null;
  }
  window.yavchn.nextStory = nextStory;

  document.addEventListener('keydown', function (e) {
    if (!window.yavchn.plainKey(e)) return;
    if (e.key === ']') {
      var next = nextStory(document.querySelector('.win.active:not([hidden])'));
      if (next) { e.preventDefault(); next.click(); }
      return;
    }
    var list = rows();
    if (!list.length) return;
    var idx = list.indexOf(focused());
    switch (e.key) {
      case 'j':
        e.preventDefault();
        mark(list[Math.min(idx + 1, list.length - 1)], true);
        break;
      case 'k':
        e.preventDefault();
        mark(list[Math.max(idx - 1, 0)], true);
        break;
      case 'Enter':
        // Enter on a control or a link does what it always does.
        if (e.target.closest && e.target.closest('a, button')) return;
        var link = idx >= 0 && list[idx].querySelector('a[data-win-open]');
        if (!link) return;
        e.preventDefault();
        link.click();
        break;
    }
  });
})();
