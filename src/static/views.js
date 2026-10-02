/* The URL selects the current view; the settings form saves its default. */
(function () {
 'use strict';
 var mode = document.body.dataset.view || 'classic';
 function listPath(path) { return /^\/(hn|lobsters)(\/|$)/.test(path) || /^\/(pinned|find)(\/|$)/.test(path); }
 function decorate() {
  if (mode === 'window' && !new URLSearchParams(location.search).has('view')) return;
  document.querySelectorAll('a[href]').forEach(function(a) {
   var u = new URL(a.href, location.href);
   if (u.origin !== location.origin || !listPath(u.pathname) || a.hasAttribute('data-reader-new') || a.hasAttribute('data-win-back') || a.hasAttribute('data-win-restore') || a.hasAttribute('data-close-all')) return;
   if (!u.searchParams.has('view') && !u.searchParams.has('open')) { u.searchParams.set('view', mode); a.href = u.pathname + u.search + u.hash; }
  });
 }
 // A view change is a full navigation, even when its target shares regions.
 document.addEventListener('submit', function(e) {
  if (!e.target.matches('#view-classic-form, #view-window-form')) return;
  var input = e.target.elements.target;
  var chosen = e.target.elements.view.value;
  if (document.querySelector('.story-list')) {
   var u = new URL(location.href);
   if (chosen === 'classic') {
    var front = document.querySelector('.win.active:not([hidden]) .story[data-story-key]');
    if (front) { input.value = front.getAttribute('data-applet-page'); return; }
    var appPage = document.querySelector('.win.active:not([hidden]) .win-head a[data-win-action="page"]');
    if (appPage) { var pageURL = new URL(appPage.href); input.value = pageURL.pathname + pageURL.search + pageURL.hash; return; }
    [...u.searchParams.keys()].forEach(function(k) { if (['open','top','min'].includes(k) || /^[pr]\./.test(k)) u.searchParams.delete(k); });
   }
   u.searchParams.set('view', chosen);
   input.value = u.pathname + u.search + u.hash;
  }
 }, true);
 decorate();
 document.addEventListener('pudl:regions-swap', decorate);
 document.addEventListener('yavchn:rows-appended', decorate);
})();
