(function () {
  var KEY = 'yavchn-focus';
  var btn = document.querySelector('.focus-button');

  function isOn() {
    return document.documentElement.classList.contains('focus-mode');
  }

  function set(on) {
    document.documentElement.classList.toggle('focus-mode', !!on);
    try { localStorage.setItem(KEY, on ? '1' : '0'); } catch (e) {}
    if (btn) btn.setAttribute('aria-pressed', on ? 'true' : 'false');
  }

  function inEditable(t) {
    if (!t) return false;
    var tag = (t.tagName || '').toLowerCase();
    if (tag === 'input' || tag === 'textarea' || tag === 'select') return true;
    return !!t.isContentEditable;
  }

  // Sync button's aria-pressed to the class (which may have been applied
  // pre-paint by the inline head script in index.html.tmpl).
  if (btn) {
    btn.setAttribute('aria-pressed', isOn() ? 'true' : 'false');
    btn.addEventListener('click', function () { set(!isOn()); });
  }

  document.addEventListener('keydown', function (e) {
    if (e.ctrlKey || e.altKey || e.metaKey || e.shiftKey) return;
    if (e.key !== 'f') return;
    if (inEditable(e.target)) return;
    e.preventDefault();
    set(!isOn());
  });
})();
