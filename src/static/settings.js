/* Settings uses the same preferences as the menus and discussion controls. */
(function () {
  'use strict';
  function mark(root, selector, value) {
    root.querySelectorAll(selector).forEach(function (button) {
      button.setAttribute('aria-pressed', String(button.value === value));
    });
  }
  function sync() {
    document.querySelectorAll('[data-settings]').forEach(function (root) {
      root.querySelectorAll('[data-settings-js]').forEach(function (el) { el.disabled = false; });
      mark(root, '[data-setting="theme"]', window.pudlThemePreference());
      mark(root, '[data-setting="sort"]', window.yavchn.sort.get());
    });
  }
  function status(root, text) { root.querySelector('.settings-status').textContent = text; }
  async function saveView(form, value) {
    var root = form.closest('[data-settings]');
    var placement = form.matches('.settings-placement-form');
    var name = placement ? 'placement' : 'view';
    var buttons = form.querySelectorAll('button[name="' + name + '"]');
    if (buttons[0].disabled) return;
    var body = new URLSearchParams({ [name]: value, target: '/settings' });
    buttons.forEach(function (button) { button.disabled = true; });
    status(root, 'Saving preference…');
    try {
      var response = await fetch(form.action, { method: 'POST', credentials: 'same-origin', headers: { Accept: 'application/json' }, body: body });
      if (!response.ok) throw new Error('Save failed');
      document.querySelectorAll(placement ? '.settings-placement-form' : '.settings-view-form').forEach(function (other) {
        other.dataset.savedView = value;
        mark(other, 'button[name="' + name + '"]', value);
      });
      status(root, placement ? 'Article window preference saved.' : 'Default view saved.');
    } catch (err) {
      status(root, placement ? 'The preference could not be saved. Try again.' : 'The default view could not be saved. Try again.');
    } finally { buttons.forEach(function (button) { button.disabled = false; }); }
  }
  document.addEventListener('click', function (e) {
    var button = e.target.closest('button[data-setting]');
    if (!button) return;
    var root = button.closest('[data-settings]');
    if (!root) return;
    if (button.dataset.setting === 'theme') { window.pudlSetTheme(button.value); status(root, 'Theme saved.'); }
    if (button.dataset.setting === 'sort') { window.yavchn.sort.set(button.value); status(root, 'Comment order saved.'); }
  });
  document.addEventListener('submit', function (e) {
    if (!e.target.matches('.settings-side-form') || !e.submitter) return;
    e.preventDefault();
    var root = e.target.closest('[data-settings]');
    status(root, 'Saving preference…');
    window.yavchn.setSidebarSide(e.submitter.value).then(function () { status(root, 'Story list side saved.'); },
      function () { status(root, 'The preference could not be saved. Try again.'); });
  });
  document.addEventListener('submit', function (e) {
    if (!e.target.matches('.settings-view-form, .settings-placement-form')) return;
    e.preventDefault();
    if (e.submitter) saveView(e.target, e.submitter.value);
  });
  document.addEventListener('pudl:window-open', sync);
  document.addEventListener('pudl:theme-change', sync);
  document.addEventListener('yavchn:sort-change', sync);
  sync();
})();
