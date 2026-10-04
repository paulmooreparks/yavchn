/* Settings, the user lookup and profiles have no working commands, but every
   window still has its identity menu: Open as a page, Copy link and Close
   window, as PUDL's menu-bar conventions give each applet. */
(function () {
  'use strict';
  [['settings', 'Settings', 'Copy settings link', '/settings'],
    ['lookup', 'Look up a user', 'Copy lookup link', '/user'],
    ['profile', 'Profile', 'Copy profile link', '']].forEach(function (a) {
    window.pudlApplets.register(a[0], { init: function (root) {
      return { menus: function () {
        return { titles: [{ label: a[1], items: window.yavchn.identityMenu(root, a[2], a[3] || location.href) }] };
      } };
    } });
  });
})();
