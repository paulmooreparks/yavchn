/* The host's side of the applets' state. PUDL keeps no applet state: it
   asks the host for the state before an applet starts, with
   pudl:applet-state, and says when the state changes, with
   pudl:applet-change. YAVCHN keeps each applet's state in this browser
   under the mount's data-state-key: a story's key for a story, so each
   story has its own place, or the applet's name for one there is only one
   of, so its window and its page share it.

   A reader closing a window means they are done with it, so its state is
   forgotten, unless the mount carries data-state-keep, as the replies
   watcher does for the user it watches. A window that Next replaced, or
   that Back moved past, keeps its state, since the reader may well
   return. The last hundred are kept. */
(function () {
  'use strict';
  var KEY = 'yavchn-applet-state';
  var CAP = 100;

  function kept() {
    try { return JSON.parse(localStorage.getItem(KEY) || '{}') || {}; } catch (e) { return {}; }
  }

  function keep(all) {
    var keys = Object.keys(all);
    if (keys.length > CAP) {
      keys.sort(function (a, b) { return (all[a].t || 0) - (all[b].t || 0); });
      keys.slice(0, keys.length - CAP).forEach(function (k) { delete all[k]; });
    }
    try { localStorage.setItem(KEY, JSON.stringify(all)); } catch (e) { /* not kept, then */ }
  }

  function keyOf(mount) { return mount && mount.getAttribute && mount.getAttribute('data-state-key'); }

  document.addEventListener('pudl:applet-state', function (e) {
    var k = keyOf(e.target);
    var s = k && kept()[k];
    if (s && s.s != null) e.detail.state = s.s;
  });

  document.addEventListener('pudl:applet-change', function (e) {
    var k = keyOf(e.target);
    if (!k) return;
    var all = kept();
    if (e.detail.state) all[k] = { s: e.detail.state, t: Date.now() };
    else delete all[k];
    keep(all);
  });

  document.addEventListener('pudl:window-close', function (e) {
    var why = e.detail && e.detail.reason;
    if (why !== 'button' && why !== 'key' && why !== 'script') return;
    var all = kept(), changed = false;
    (e.target.querySelectorAll ? e.target.querySelectorAll('[data-state-key]:not([data-state-keep])') : []).forEach(function (m) {
      if (all[keyOf(m)]) { delete all[keyOf(m)]; changed = true; }
    });
    if (changed) keep(all);
  });
})();
