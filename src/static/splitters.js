(function () {
  var layout = document.querySelector('.layout');
  if (!layout) return;

  function attach(splitter, axis, cssVar, storageKey, minAFn, minBFn) {
    if (!splitter) return;

    // Make the splitter keyboard-focusable + advertise the ARIA range so
    // screen readers report the current value.
    splitter.tabIndex = 0;
    splitter.setAttribute('aria-valuemin', '0');
    splitter.setAttribute('aria-valuemax', '100');
    updateAria(splitter, cssVar, axis);

    // -------- pointer (mouse / touch / pen) drag --------
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
        updateAria(splitter, cssVar, axis);
      }

      function onUp(ev) {
        splitter.removeEventListener('pointermove', onMove);
        splitter.removeEventListener('pointerup', onUp);
        splitter.removeEventListener('pointercancel', onUp);
        try { splitter.releasePointerCapture(ev.pointerId); } catch (e) {}
        splitter.classList.remove('active');
        persist(cssVar, storageKey);
      }

      splitter.addEventListener('pointermove', onMove);
      splitter.addEventListener('pointerup', onUp);
      splitter.addEventListener('pointercancel', onUp);
    });

    // -------- keyboard resize --------
    // Arrow keys nudge by 2% (5% with shift). Home/End snap to min/max.
    // Axis decides which arrows apply: horizontal splitter (axis=x) uses
    // Left/Right; vertical splitter (axis=y) uses Up/Down.
    splitter.addEventListener('keydown', function (e) {
      if (e.ctrlKey || e.altKey || e.metaKey) return;
      var step = e.shiftKey ? 5 : 2;
      var dir = 0;
      var snap = null;
      var key = e.key;
      if (axis === 'x') {
        if (key === 'ArrowLeft') dir = -1;
        else if (key === 'ArrowRight') dir = 1;
        else if (key === 'Home') snap = 'min';
        else if (key === 'End') snap = 'max';
        else return;
      } else {
        if (key === 'ArrowUp') dir = -1;
        else if (key === 'ArrowDown') dir = 1;
        else if (key === 'Home') snap = 'min';
        else if (key === 'End') snap = 'max';
        else return;
      }
      e.preventDefault();
      var rect = layout.getBoundingClientRect();
      var total = axis === 'x' ? rect.width : rect.height;
      if (total <= 0) return;
      var minA = typeof minAFn === 'function' ? minAFn() : minAFn;
      var minB = typeof minBFn === 'function' ? minBFn() : minBFn;
      var minPct = (minA / total) * 100;
      var maxPct = ((total - minB) / total) * 100;
      var pct;
      if (snap === 'min') pct = minPct;
      else if (snap === 'max') pct = maxPct;
      else pct = currentPercent(cssVar, axis) + dir * step;
      pct = Math.max(minPct, Math.min(maxPct, pct));
      layout.style.setProperty(cssVar, pct.toFixed(2) + '%');
      updateAria(splitter, cssVar, axis);
      persist(cssVar, storageKey);
    });
  }

  // currentPercent reads the splitter's current position. If the CSS var
  // is set (from a prior drag or persisted load), parse it directly. Else
  // measure the affected pane's actual size as a fraction of the layout.
  function currentPercent(cssVar, axis) {
    var raw = (layout.style.getPropertyValue(cssVar) || '').trim();
    if (raw.endsWith('%')) {
      var n = parseFloat(raw);
      if (!isNaN(n)) return n;
    }
    var paneSel = cssVar === '--list-w' ? '.pane-list' : '.pane-article';
    var pane = document.querySelector(paneSel);
    var rect = layout.getBoundingClientRect();
    if (pane) {
      var paneRect = pane.getBoundingClientRect();
      var size = axis === 'x' ? paneRect.width : paneRect.height;
      var total = axis === 'x' ? rect.width : rect.height;
      if (total > 0) return (size / total) * 100;
    }
    return 50;
  }

  function updateAria(splitter, cssVar, axis) {
    var pct = currentPercent(cssVar, axis);
    splitter.setAttribute('aria-valuenow', Math.round(pct));
  }

  function persist(cssVar, storageKey) {
    try {
      localStorage.setItem(storageKey, layout.style.getPropertyValue(cssVar));
    } catch (e) {}
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
