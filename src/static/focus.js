(function () {
  var KEY = 'yavchn-focus';

  function set(on) {
    document.documentElement.classList.toggle('focus-mode', !!on);
    try { localStorage.setItem(KEY, on ? '1' : '0'); } catch (e) {}
  }

  function inEditable(t) {
    if (!t) return false;
    var tag = (t.tagName || '').toLowerCase();
    if (tag === 'input' || tag === 'textarea' || tag === 'select') return true;
    return !!t.isContentEditable;
  }

  document.addEventListener('keydown', function (e) {
    if (e.ctrlKey || e.altKey || e.metaKey || e.shiftKey) return;
    if (e.key !== 'f') return;
    if (inEditable(e.target)) return;
    e.preventDefault();
    set(!document.documentElement.classList.contains('focus-mode'));
  });
})();
