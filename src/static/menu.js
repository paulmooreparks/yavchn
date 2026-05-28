(function () {
  var btn = document.querySelector('.hamburger-btn');
  if (!btn) return;
  var actions = document.getElementById('header-actions');
  if (!actions) return;
  var html = document.documentElement;

  function open() {
    html.classList.add('menu-open');
    btn.setAttribute('aria-expanded', 'true');
  }
  function close() {
    html.classList.remove('menu-open');
    btn.setAttribute('aria-expanded', 'false');
  }

  btn.addEventListener('click', function (e) {
    e.stopPropagation();
    if (html.classList.contains('menu-open')) close();
    else open();
  });

  // Click inside the menu closes it after the action fires (so the user
  // doesn't have to re-tap a close button). Click outside closes too.
  document.addEventListener('click', function (e) {
    if (!html.classList.contains('menu-open')) return;
    if (e.target.closest('.hamburger-btn')) return;
    if (actions.contains(e.target)) {
      // Let the inner click handler run, then close on next tick.
      setTimeout(close, 0);
      return;
    }
    close();
  });

  document.addEventListener('keydown', function (e) {
    if (e.key === 'Escape' && html.classList.contains('menu-open')) close();
  });
})();
