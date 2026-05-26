(function () {
  var list = document.querySelector('.pane-list .stories');
  if (!list) return;

  // Items are re-queried on every keypress so appended rows from infinite
  // scroll (yavchn-5) participate in navigation automatically.
  function getItems() {
    return Array.prototype.slice.call(list.querySelectorAll('.story'));
  }

  var initial = getItems();
  if (!initial.length) return;

  var focusIdx = -1;
  for (var i = 0; i < initial.length; i++) {
    if (initial[i].classList.contains('selected')) { focusIdx = i; break; }
  }
  if (focusIdx < 0) focusIdx = 0;
  applyFocus(initial);

  function applyFocus(items) {
    for (var i = 0; i < items.length; i++) {
      items[i].classList.toggle('focused', i === focusIdx);
    }
    var li = items[focusIdx];
    if (li && typeof li.scrollIntoView === 'function') {
      li.scrollIntoView({ block: 'nearest' });
    }
  }

  function openFocused(items) {
    var li = items[focusIdx];
    if (!li) return;
    var a = li.querySelector('.title a');
    if (a && a.href) window.location.href = a.href;
  }

  function inEditable(target) {
    if (!target) return false;
    var tag = (target.tagName || '').toLowerCase();
    if (tag === 'input' || tag === 'textarea' || tag === 'select') return true;
    if (target.isContentEditable) return true;
    return false;
  }

  document.addEventListener('keydown', function (e) {
    if (e.ctrlKey || e.altKey || e.metaKey) return;
    if (inEditable(e.target)) return;

    var items = getItems();
    if (!items.length) return;

    switch (e.key) {
      case 'j':
      case 'ArrowDown':
        if (focusIdx < items.length - 1) {
          focusIdx++;
          applyFocus(items);
        }
        e.preventDefault();
        break;
      case 'k':
      case 'ArrowUp':
        if (focusIdx > 0) {
          focusIdx--;
          applyFocus(items);
        }
        e.preventDefault();
        break;
      case 'Enter':
        if (e.target === document.body) {
          e.preventDefault();
          openFocused(items);
        }
        break;
    }
  });
})();
