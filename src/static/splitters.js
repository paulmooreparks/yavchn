(function () {
  var layout = document.querySelector('.layout');
  if (!layout) return;

  function attach(splitter, axis, cssVar, storageKey, minAFn, minBFn) {
    if (!splitter) return;

    splitter.addEventListener('pointerdown', function (e) {
      e.preventDefault();
      splitter.setPointerCapture(e.pointerId);
      splitter.classList.add('active');

      var minA = typeof minAFn === 'function' ? minAFn() : minAFn;
      var minB = typeof minBFn === 'function' ? minBFn() : minBFn;

      function onMove(ev) {
        var rect = layout.getBoundingClientRect();
        var size, total;
        if (axis === 'x') {
          size = ev.clientX - rect.left;
          total = rect.width;
          size = Math.max(minA, Math.min(total - minB, size));
        } else {
          size = ev.clientY - rect.top;
          total = rect.height;
          size = Math.max(minA, Math.min(total - minB, size));
        }
        // Store as a percentage so the layout adapts to viewport changes
        // (different monitor, window resize) without going off-screen.
        var pct = total > 0 ? (size / total) * 100 : 50;
        layout.style.setProperty(cssVar, pct.toFixed(2) + '%');
      }

      function onUp(ev) {
        splitter.removeEventListener('pointermove', onMove);
        splitter.removeEventListener('pointerup', onUp);
        splitter.removeEventListener('pointercancel', onUp);
        try { splitter.releasePointerCapture(ev.pointerId); } catch (e) {}
        splitter.classList.remove('active');
        try {
          localStorage.setItem(storageKey, layout.style.getPropertyValue(cssVar));
        } catch (e) {}
      }

      splitter.addEventListener('pointermove', onMove);
      splitter.addEventListener('pointerup', onUp);
      splitter.addEventListener('pointercancel', onUp);
    });
  }

  // Minimums depend on viewport: narrow mode lets the list shrink more so
  // the right pane has room. Evaluated per drag-start so a rotation /
  // window-resize picks up the new floor.
  function narrow() { return window.innerWidth < 800; }
  function listMin()  { return narrow() ? 90  : 220; }
  function rightMin() { return narrow() ? 120 : 320; }

  attach(layout.querySelector('.splitter-v'), 'x', '--list-w', 'yavchn-list-w', listMin, rightMin);
  attach(layout.querySelector('.splitter-h'), 'y', '--article-h', 'yavchn-article-h', 140, 140);
})();
