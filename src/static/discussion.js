(function () {
  // Delegated click handler on the discussion pane: click anywhere on a
  // comment header (but not on a link / button) toggles the comment's
  // collapsed state. Survives pane-swap (yavchn-12) and lazy-load
  // (reader.js) because the listener is on a stable ancestor.
  var pane = document.querySelector('.pane-discussion');
  if (!pane) return;

  pane.addEventListener('click', function (e) {
    // Let links and buttons through (HN-link anchor, future affordances).
    if (e.target.closest('a, button')) return;
    var header = e.target.closest('.comment-header');
    if (!header) return;
    var comment = header.closest('.comment');
    if (!comment) return;
    comment.classList.toggle('collapsed');
  });
})();
