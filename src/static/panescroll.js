(function () {
  // Arrow keys scroll the focused pane. The browser scrolls the nearest
  // scrollable ancestor of document.activeElement, so the only thing needed
  // is for each pane's scroll container to be focusable and to take focus
  // when the user clicks into it. List selection stays on j/k (see keys.js).
  //
  // Scroll containers differ per pane: the story list scrolls on the
  // .pane-list section itself, while the article and discussion panes scroll
  // on their inner .pane-body. The finder reuses the same classes.
  var selectors = [
    '.pane-list',
    '.pane-article .pane-body',
    '.pane-discussion .pane-body'
  ];

  var panes = [];
  selectors.forEach(function (sel) {
    var nodes = document.querySelectorAll(sel);
    for (var i = 0; i < nodes.length; i++) panes.push(nodes[i]);
  });
  if (!panes.length) return;

  panes.forEach(function (pane) {
    // tabindex=-1: focusable programmatically and via click, but kept out of
    // the Tab order so tabbing still steps through links and controls.
    if (!pane.hasAttribute('tabindex')) pane.setAttribute('tabindex', '-1');
    // Suppress the focus ring on the container; arrow-scroll is the point,
    // not a visible focus target. Interactive children keep their own ring.
    pane.style.outline = 'none';

    pane.addEventListener('mousedown', function (e) {
      // Don't steal focus from a real control the user is clicking on.
      if (e.target.closest('a, button, input, textarea, select, [contenteditable]')) return;
      // Defer so we don't fight the browser's own mousedown focus handling.
      var p = pane;
      setTimeout(function () {
        if (!p.contains(document.activeElement)) p.focus({ preventScroll: true });
      }, 0);
    });
  });
})();
