(function () {
  // Delegated click handler: click anywhere on a comment header (but not
  // on a link / button) toggles the comment's collapsed state. Listens on
  // document so it survives pane-swap (yavchn-12) and lazy-load.
  document.addEventListener('click', function (e) {
    if (e.target.closest('a, button')) return;
    var header = e.target.closest('.pane-discussion .comment-header');
    if (!header) return;
    var comment = header.closest('.comment');
    if (!comment) return;
    comment.classList.toggle('collapsed');
  });

  // Highlight comments newer than the visitor's last-visit timestamp for
  // this story. Fires on yavchn:loaded (dispatched by reader.js after the
  // discussion fragment is injected) -- runs whether the discussion was
  // lazy-loaded on initial pageview or replaced via pane-swap.
  document.addEventListener('yavchn:loaded', function (e) {
    var body = e.target;
    var pane = body.closest('.pane-discussion');
    if (!pane) return;
    var storyID = pane.dataset.discussionId;
    if (!storyID) return;
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
