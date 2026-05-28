(function () {
  function storyIDOfComment(comment) {
    var pane = comment.closest('.pane-discussion');
    return pane ? pane.dataset.discussionId : '';
  }

  function loadCollapsed(storyID) {
    if (!storyID) return [];
    try {
      var raw = localStorage.getItem('yavchn-collapsed:' + storyID);
      if (!raw) return [];
      var arr = JSON.parse(raw);
      return Array.isArray(arr) ? arr : [];
    } catch (err) { return []; }
  }

  function saveCollapsed(storyID, ids) {
    if (!storyID) return;
    // Cap so storage doesn't grow unboundedly on long busy threads.
    if (ids.length > 500) ids = ids.slice(-500);
    try { localStorage.setItem('yavchn-collapsed:' + storyID, JSON.stringify(ids)); } catch (err) {}
  }

  // Toggle a comment's collapsed state + persist the change. Shared between
  // the click handler (clicking the comment header) and the keyboard handler
  // (pressing `c` while a comment is focused), so the two paths can't drift.
  function toggleCollapse(comment) {
    if (!comment) return;
    var nowCollapsed = comment.classList.toggle('collapsed');
    var id = comment.dataset.id;
    if (!id) return;
    var storyID = storyIDOfComment(comment);
    if (!storyID) return;
    var ids = loadCollapsed(storyID);
    var idx = ids.indexOf(id);
    if (nowCollapsed && idx < 0) ids.push(id);
    else if (!nowCollapsed && idx >= 0) ids.splice(idx, 1);
    saveCollapsed(storyID, ids);
  }

  // Delegated click handler: click anywhere on a comment header (but not
  // on a link / button) toggles the comment's collapsed state. Listens on
  // document so it survives pane-swap (yavchn-12) and lazy-load.
  document.addEventListener('click', function (e) {
    if (e.target.closest('a, button')) return;
    var header = e.target.closest('.pane-discussion .comment-header');
    if (!header) return;
    toggleCollapse(header.closest('.comment'));
  });

  // Highlight comments newer than the visitor's last-visit timestamp for
  // this story. Fires on yavchn:loaded (dispatched by reader.js after the
  // discussion fragment is injected) -- runs whether the discussion was
  // lazy-loaded on initial pageview or replaced via pane-swap.
  // Jump-to-next / -previous top-level comment via n / N (shift). Index is
  // reset on every yavchn:loaded so each new story starts at the first
  // top-level comment.
  var topIdx = -1;

  function topLevelComments() {
    return Array.prototype.slice.call(
      document.querySelectorAll('.pane-discussion .discussion-content > .thread > .comment')
    );
  }

  function inEditable(t) {
    if (!t) return false;
    var tag = (t.tagName || '').toLowerCase();
    if (tag === 'input' || tag === 'textarea' || tag === 'select') return true;
    return !!t.isContentEditable;
  }

  // Mark a single top-level comment as keyboard-focused (stripping the
  // class from any other focused comment). Lets the visitor see which
  // comment `c` will act on.
  function setFocus(comments, idx) {
    for (var i = 0; i < comments.length; i++) {
      comments[i].classList.toggle('focused', i === idx);
    }
  }

  document.addEventListener('keydown', function (e) {
    if (e.ctrlKey || e.altKey || e.metaKey) return;
    if (inEditable(e.target)) return;

    // n / N: navigate top-level comments + mark focus.
    if (e.key === 'n' || e.key === 'N') {
      var comments = topLevelComments();
      if (!comments.length) return;
      if (e.key === 'n') {
        topIdx = topIdx < comments.length - 1 ? topIdx + 1 : comments.length - 1;
      } else {
        topIdx = topIdx > 0 ? topIdx - 1 : 0;
      }
      setFocus(comments, topIdx);
      var c = comments[topIdx];
      if (c && typeof c.scrollIntoView === 'function') {
        c.scrollIntoView({ block: 'start', behavior: 'smooth' });
      }
      e.preventDefault();
      return;
    }

    // c: toggle collapse on the focused top-level comment.
    if (e.key === 'c' && !e.shiftKey) {
      var topComments = topLevelComments();
      if (!topComments.length || topIdx < 0 || topIdx >= topComments.length) return;
      toggleCollapse(topComments[topIdx]);
      e.preventDefault();
    }
  });

  document.addEventListener('yavchn:loaded', function (e) {
    topIdx = -1;
  });

  document.addEventListener('yavchn:loaded', function (e) {
    var body = e.target;
    var pane = body.closest('.pane-discussion');
    if (!pane) return;
    var storyID = pane.dataset.discussionId;
    if (!storyID) return;

    // Restore persisted collapse state.
    var collapsed = loadCollapsed(storyID);
    if (collapsed.length) {
      var set = {};
      for (var ci = 0; ci < collapsed.length; ci++) set[collapsed[ci]] = true;
      var coms = body.querySelectorAll('.comment[data-id]');
      for (var k = 0; k < coms.length; k++) {
        if (set[coms[k].dataset.id]) coms[k].classList.add('collapsed');
      }
    }

    // Highlight comments newer than last visit.
    var key = 'yavchn-last-visit:' + storyID;
    var prev = 0;
    try {
      prev = parseInt(localStorage.getItem(key) || '0', 10) || 0;
    } catch (err) {}

    if (prev > 0) {
      var comments = body.querySelectorAll('.comment[data-ts]');
      for (var i = 0; i < comments.length; i++) {
        var ts = parseInt(comments[i].dataset.ts || '0', 10);
        if (ts > prev) comments[i].classList.add('comment-new');
      }
    }

    // Save now() so the next visit highlights only what arrived after this one.
    try { localStorage.setItem(key, String(Math.floor(Date.now() / 1000))); } catch (err) {}
  });
})();
