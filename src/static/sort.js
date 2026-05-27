(function () {
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

  // Reorder top-level comments only. Children stay chronologically nested
  // inside their thread (matches HN's own UX: changing the sort changes
  // which discussions surface first, not how replies within a discussion
  // are laid out).
  function applySort(pane) {
    var sel = pane.querySelector('.comment-sort');
    if (sel && sel.value !== getPref()) sel.value = getPref();
    var root = pane.querySelector('.discussion-content > .thread');
    if (!root) return;
    var items = Array.prototype.slice.call(root.children);
    if (items.length < 2) return;

    // Stamp the server-given order once so "Best" can restore it after the
    // visitor flips to Newest / Oldest and back.
    for (var i = 0; i < items.length; i++) {
      if (!items[i].dataset.serverOrder) items[i].dataset.serverOrder = String(i);
    }

    var mode = getPref();
    items.sort(function (a, b) {
      if (mode === 'best') {
        return Number(a.dataset.serverOrder) - Number(b.dataset.serverOrder);
      }
      var ta = Number(a.dataset.ts || 0);
      var tb = Number(b.dataset.ts || 0);
      if (mode === 'newest') return tb - ta;
      return ta - tb;
    });
    for (var j = 0; j < items.length; j++) root.appendChild(items[j]);
  }

  // Sync the dropdown's value to the persisted pref even when there's no
  // discussion content yet (e.g. the loading-state placeholder).
  function syncSelect(pane) {
    var sel = pane.querySelector('.comment-sort');
    if (sel) sel.value = getPref();
  }

  // Fires after reader.js injects the discussion fragment into .pane-body.
  document.addEventListener('yavchn:loaded', function (e) {
    var pane = e.target.closest('.pane-discussion');
    if (!pane) return;
    applySort(pane);
  });

  // Initial page-load: pane is server-rendered with the discussion fetch
  // still pending. Sync select value now so it shows the right option
  // before discussion content arrives.
  var initialPane = document.querySelector('.pane-discussion');
  if (initialPane) syncSelect(initialPane);

  // Document-level so it survives swap.js replacing the discussion section.
  document.addEventListener('change', function (e) {
    var sel = e.target.closest('.pane-discussion .comment-sort');
    if (!sel) return;
    setPref(sel.value);
    var pane = sel.closest('.pane-discussion');
    if (pane) applySort(pane);
  });
})();
