/* Where each applet's script is. PUDL's applet runtime loads a script the
   first time a mount of its applet appears, so a page that never opens
   the replies watcher never loads it. The story applet is in every page
   and loads with it. */
(function () {
  'use strict';
  window.yavchn = window.yavchn || {};
  // Explicit applet menus share the same supported identity operations.
  window.yavchn.identityMenu = function (root, label, pageURL) {
    var win = root.closest('.win');
    var items = [];
    if (win) {
      var commands = window.pudlWindows.menuCommands(win.getAttribute('data-win'));
      var page = commands.find(function (c) { return c.id === 'page'; });
      var copy = commands.find(function (c) { return c.id === 'copy-link'; });
      var close = commands.find(function (c) { return c.id === 'close'; });
      if (page) items.push(Object.assign({}, page, { label: 'Open as a page' }));
      if (copy) items.push(Object.assign({}, copy, { label: label }));
      if (close) items.push('-', Object.assign({}, close, { label: 'Close window' }));
    } else {
      items.push({ label: label, run: function () {
        if (navigator.clipboard) navigator.clipboard.writeText(new URL(pageURL || location.href, location.href).href).catch(function () {});
      } });
    }
    return items;
  };
  var version = document.querySelector('script[data-asset-version]').getAttribute('data-asset-version');
  window.pudlApplets.define('story', {});
  // Settings, the user lookup and profiles share one small script that gives
  // each the identity menu every window has.
  ['settings', 'lookup', 'profile'].forEach(function (name) {
    window.pudlApplets.define(name, { src: '/static/applets/identity.js?v=' + version, ver: '1' });
  });
  window.pudlApplets.define('account', { src: '/static/applets/account.js?v=' + version, page: '/account?view=classic', ver: '1' });
  window.pudlApplets.define('replies', { src: '/static/applets/replies.js?v=' + version, page: '/applets/replies', ver: '1' });
  window.pudlApplets.define('hiring', { src: '/static/applets/hiring.js?v=' + version, page: '/applets/hiring', ver: '1' });
})();
