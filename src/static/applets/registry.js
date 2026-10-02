/* Where each applet's script is. PUDL's applet runtime loads a script the
   first time a mount of its applet appears, so a page that never opens
   the replies watcher never loads it. The story applet is in every page
   and loads with it. */
(function () {
  'use strict';
  window.pudlApplets.define('story', {});
  window.pudlApplets.define('replies', { src: '/static/applets/replies.js', page: '/applets/replies', ver: '1' });
  window.pudlApplets.define('hiring', { src: '/static/applets/hiring.js', page: '/applets/hiring', ver: '1' });
})();
