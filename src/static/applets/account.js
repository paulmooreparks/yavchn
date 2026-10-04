/* Account content uses PUDL's applet lifecycle in a window or Classic page. */
(function () {
  'use strict';
  window.pudlApplets.register('account', { init: function (root, opts) {
    window.yavchnAccountUI(root);
    function returning(event) {
      var input = event.target.querySelector('input[name="return_to"]');
      if (input) input.value = opts.host === 'window' ? location.pathname + location.search : '/account?view=classic';
    }
    root.addEventListener('submit', returning);
    return {
      menus: function () {
        var items = [];
        var login = root.querySelector('form[action="/auth/github"]');
        var exported = root.querySelector('a[href="/account/export"]');
        var imported = root.querySelector('[data-account-import]');
        var logout = root.querySelector('form[action="/account/session"]');
        var revoke = root.querySelector('form[action="/account/sessions"]');
        if (login) items.push({ label: login.querySelector('button').textContent, run: function () { login.requestSubmit(); } });
        if (imported && !imported.hidden) items.push({ label: "Import this browser's anonymous data", run: function () { imported.click(); } });
        if (exported) items.push({ label: 'Export account data', run: function () { exported.click(); } });
        if (revoke) items.push({ label: 'Sign out other devices', run: function () { revoke.requestSubmit(); } });
        if (logout) items.push({ label: 'Sign out this browser', run: function () { logout.requestSubmit(); } });
        var titles = [{ label: 'Account', items: window.yavchn.identityMenu(root, 'Copy account link', '/account?view=classic') }];
        if (items.length) titles.push({ label: 'Manage', items: items });
        return { titles: titles };
      },
      destroy: function () { root.removeEventListener('submit', returning); }
    };
  } });
})();
