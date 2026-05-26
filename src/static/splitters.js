(function () {
  var layout = document.querySelector('.layout');
  if (!layout) return;

  function attach(splitter, axis, cssVar, storageKey, minA, minB) {
    if (!splitter) return;

    splitter.addEventListener('pointerdown', function (e) {
      e.preventDefault();
      splitter.setPointerCapture(e.pointerId);
      splitter.classList.add('active');

      function onMove(ev) {
        var rect = layout.getBoundingClientRect();
        var size;
        if (axis === 'x') {
          size = ev.clientX - rect.left;
          size = Math.max(minA, Math.min(rect.width - minB, size));
        } else {
          size = ev.clientY - rect.top;
          size = Math.max(minA, Math.min(rect.height - minB, size));
        }
        layout.style.setProperty(cssVar, size + 'px');
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

  attach(layout.querySelector('.splitter-v'), 'x', '--list-w', 'yavchn-list-w', 220, 320);
  attach(layout.querySelector('.splitter-h'), 'y', '--article-h', 'yavchn-article-h', 140, 140);
})();
