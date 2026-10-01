/* The order of a discussion's top-level comments: the source's own
   ("Best"), newest first or oldest first. The choice is the reader's, kept
   under yavchn-comment-sort, and every story follows it. Replies stay in
   the order the source gave them, as HN's own sort does. */
(function () {
  'use strict';
  var KEY = 'yavchn-comment-sort';

  function getPref() {
    try {
      var p = localStorage.getItem(KEY);
      if (p === 'newest' || p === 'oldest') return p;
    } catch (e) {}
    return 'best';
  }

  function setPref(p) {
    try { localStorage.setItem(KEY, p); } catch (e) {}
  }

  function syncButtons(scope) {
    var mode = getPref();
    scope.querySelectorAll('.story-sort [data-sort]').forEach(function (b) {
      b.setAttribute('aria-pressed', b.dataset.sort === mode ? 'true' : 'false');
    });
  }

  function applySort(story) {
    var root = story.querySelector('.discussion-content > .thread');
    if (!root) return;
    var items = Array.prototype.slice.call(root.children);
    if (items.length < 2) return;
    // Stamp the server's order once, so "Best" can restore it.
    items.forEach(function (it, i) { if (!it.dataset.serverOrder) it.dataset.serverOrder = String(i); });
    var mode = getPref();
    items.sort(function (a, b) {
      if (mode === 'best') return Number(a.dataset.serverOrder) - Number(b.dataset.serverOrder);
      var ta = Number(a.dataset.ts || 0), tb = Number(b.dataset.ts || 0);
      return mode === 'newest' ? tb - ta : ta - tb;
    });
    items.forEach(function (it) { root.appendChild(it); });
  }

  document.addEventListener('yavchn:loaded', function (e) {
    if (!e.target.classList.contains('story-discussion-body')) return;
    applySort(e.target.closest('.story'));
  });

  function choose(mode) {
    setPref(mode);
    syncButtons(document);
    document.querySelectorAll('.story').forEach(applySort);
  }

  window.yavchn.sort = { get: getPref, set: choose };

  document.addEventListener('click', function (e) {
    var b = e.target.closest && e.target.closest('.story-sort [data-sort]');
    if (b) choose(b.dataset.sort);
  });

  document.addEventListener('pudl:window-open', function (e) { syncButtons(e.target); });
  syncButtons(document);
})();
