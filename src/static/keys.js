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

  // Activatable = the kind of element where Enter would invoke a default
  // action other than "follow a link" (buttons fire onclick, inputs submit
  // forms). For those, let the browser do its thing. Anchors are NOT in
  // this set: Enter on the focused story should override a stray Tab-focus
  // on a story link.
  function isActivatable(target) {
    if (inEditable(target)) return true;
    var tag = (target.tagName || '').toLowerCase();
    return tag === 'button';
  }

  // Unify mouse and keyboard: clicking a story row moves the j/k focus
   // cursor onto it, so the .focused indicator always matches the user's
  // most recent interaction. Skips clicks on row-action buttons (pin /
  // dismiss) -- those have their own semantics and shouldn't drag focus.
  list.addEventListener('click', function (e) {
    if (e.button !== 0) return;
    if (e.target.closest('.pin-btn, .dismiss-btn')) return;
    var row = e.target.closest('.story');
    if (!row) return;
    var items = getItems();
    var idx = items.indexOf(row);
    if (idx < 0) return;
    focusIdx = idx;
    applyFocus(items);
  });

  document.addEventListener('keydown', function (e) {
    if (e.ctrlKey || e.altKey || e.metaKey) return;

    var items = getItems();
    if (!items.length) return;

    switch (e.key) {
      case 'j':
      case 'ArrowDown':
        if (inEditable(e.target)) return;
        if (focusIdx < items.length - 1) {
          focusIdx++;
          applyFocus(items);
        }
        e.preventDefault();
        break;
      case 'k':
      case 'ArrowUp':
        if (inEditable(e.target)) return;
        if (focusIdx > 0) {
          focusIdx--;
          applyFocus(items);
        }
        e.preventDefault();
        break;
      case 'Enter':
        if (isActivatable(e.target)) return;
        e.preventDefault();
        openFocused(items);
        break;
    }
  });
})();
