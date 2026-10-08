/* Plausible Analytics, on production only (the head loads this file only
   when YAVCHN_PLAUSIBLE_SCRIPT is set). It is Plausible's own queue, from
   its installation snippet, kept in a file rather than inline.

   Plausible counts every history.pushState as a page view, and YAVCHN
   pushes one for every window it opens or story it reads, which Plausible
   would count again and again as the list's page, since it drops the query.
   So automatic page views are off, and a page view is sent when a page
   loads and when the list's own page changes, by a link or Back. */
(function () {
  'use strict';
  window.plausible = window.plausible || function () { (window.plausible.q = window.plausible.q || []).push(arguments); };
  window.plausible.init = window.plausible.init || function (i) { window.plausible.o = i || {}; };
  window.plausible.init({ autoCapturePageviews: false });

  var counted = location.pathname;
  window.plausible('pageview');
  document.addEventListener('pudl:regions-swap', function () {
    if (location.pathname === counted) return;
    counted = location.pathname;
    window.plausible('pageview');
  });
})();
