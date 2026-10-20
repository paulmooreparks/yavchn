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
    var link = e.target.closest && e.target.closest('.story-list a[data-win-open], .story-step[data-win-open]');
    if (!link) return;
    var step = link.matches('.story-step');
    if (step) syncStep(link);
    var key = link.getAttribute('data-win-open');
    if (!key || !window.yavchn.readers.read(key, link, step ? link.closest('.win') : null)) return;
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

  /* === Previous and next story ==========================================
     A story window's Previous and Next links load the story before or
     after it into the running applet, so the reader can go up and down a
     list in one instance, and Back retraces the way. The list decides
     which: the neighbouring row that is not hidden or filtered out, even
     with the list itself hidden. */
  function neighbour(key, step) {
    var list = Array.prototype.slice.call(document.querySelectorAll('.story-list .story-row:not(.dismissed):not(.filtered)'));
    var i = list.findIndex(function (r) { return r.dataset.key === key; });
    return i >= 0 && list[i + step] ? list[i + step].querySelector('a[data-win-open]') : null;
  }

  function syncStep(a) {
    var forward = !a.matches('.story-prev');
    var words = forward ? 'Next story' : 'Previous story';
    var story = a.closest('.story');
    var to = story && a.closest('.win') ? neighbour(story.dataset.storyKey, forward ? 1 : -1) : null;
    a.hidden = !a.closest('.win');
    a.setAttribute('aria-disabled', String(!to));
    if (!to) {
      a.removeAttribute('data-win-open'); a.removeAttribute('href');
      a.setAttribute('aria-label', words);
      a.title = forward ? 'Next story: this is the last in the list' : 'Previous story: this is the first in the list';
      return;
    }
    a.setAttribute('data-win-open', to.getAttribute('data-win-open'));
    a.setAttribute('href', to.getAttribute('href'));
    a.setAttribute('aria-label', words + ': ' + to.firstChild.textContent.trim());
    a.title = a.getAttribute('aria-label') + (forward ? ' (])' : ' ([)');
  }

  function syncAllSteps() { document.querySelectorAll('.story-step').forEach(syncStep); }
  window.yavchn.onList(syncAllSteps);
  ['yavchn:rows-appended', 'yavchn:list-change', 'yavchn:story-change', 'pudl:window-open', 'pudl:windows-change'].forEach(function (name) {
    document.addEventListener(name, syncAllSteps);
  });
  /* Rows hide and pins change between those events, so the link is set
     again just before pudl-windows.js reads it. */
  document.addEventListener('click', function (e) {
    var a = e.target.closest && e.target.closest('.story-step');
    if (a) syncStep(a);
    else if (e.target.closest && e.target.closest('.story-hide, .story-show-hidden')) setTimeout(syncAllSteps, 0);
  }, true);

  /* A window's Next or Previous link, current, or null when no story lies
     that way. */
  function stepLink(win, forward) {
    var a = win && win.querySelector(forward ? '.story-next' : '.story-prev');
    if (a) syncStep(a);
    return a && !a.hidden && a.getAttribute('aria-disabled') !== 'true' ? a : null;
  }
  window.yavchn.nextStory = function (win) { return stepLink(win, true); };
  window.yavchn.previousStory = function (win) { return stepLink(win, false); };

  document.addEventListener('keydown', function (e) {
    if (!window.yavchn.plainKey(e)) return;
    if (e.key === ']' || e.key === '[') {
      var to = stepLink(document.querySelector('.win.active:not([hidden])'), e.key === ']');
      if (to) { e.preventDefault(); to.click(); }
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
