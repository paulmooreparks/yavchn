/* Infinite scroll: scrolling the story list near its end fetches the next
   page of the same list and appends its rows. The pager stays below the
   rows for a reader without script, and goes when the list ends. A new
   list from a region swap starts again from its own page. */
(function () {
  'use strict';

  function setup() {
    var list = document.querySelector('.story-list');
    var box = list && list.querySelector('.stories');
    if (!box || list.hasAttribute('data-infinite')) return;
    list.setAttribute('data-infinite', '');

    // The list's own address, without the windows or the page number.
    var q = new URLSearchParams(location.search);
    var page = parseInt(q.get('page') || '1', 10);
    if (isNaN(page) || page < 1) page = 1;
    Array.from(q.keys()).forEach(function (k) {
      if (k === 'page' || k === 'open' || k === 'top' || k === 'min' || k.indexOf('p.') === 0) q.delete(k);
    });
    var hasNext = box.dataset.hasNext === 'true';
    var inFlight = false;

    function nearBottom() {
      return list.scrollTop + list.clientHeight >= list.scrollHeight - 200;
    }

    function markEnd() {
      var pager = list.querySelector('.story-pager');
      if (pager) pager.hidden = true;
      if (!list.querySelector('.end-of-list')) {
        var div = document.createElement('div');
        div.className = 'end-of-list';
        div.textContent = 'End of the list';
        list.appendChild(div);
      }
    }

    function loadMore() {
      if (inFlight || !hasNext || !list.isConnected) return;
      inFlight = true;
      q.set('page', String(page + 1));
      fetch(location.pathname + '?' + q.toString(), { credentials: 'omit', headers: { Accept: 'text/html' } })
        .then(function (r) {
          if (!r.ok) throw new Error('upstream ' + r.status);
          return r.text();
        })
        .then(function (html) {
          var doc = new DOMParser().parseFromString(html, 'text/html');
          var next = doc.querySelector('.story-list .stories');
          var fresh = next ? next.querySelectorAll('.story-row') : [];
          if (!fresh.length) { hasNext = false; markEnd(); return; }
          fresh.forEach(function (row) {
            // A story already in the list, which moved up a page while the
            // reader scrolled, is not listed twice.
            if (!document.getElementById(row.id)) box.appendChild(document.importNode(row, true));
          });
          box.dispatchEvent(new CustomEvent('yavchn:rows-appended', { bubbles: true }));
          page += 1;
          hasNext = next.dataset.hasNext === 'true';
          if (!hasNext) markEnd();
        })
        .catch(function () { /* hasNext stays true, so scrolling tries again */ })
        .finally(function () {
          inFlight = false;
          if (hasNext && nearBottom()) loadMore();
        });
    }

    list.addEventListener('scroll', function () { if (nearBottom()) loadMore(); }, { passive: true });
    if (hasNext && nearBottom()) loadMore();
  }

  window.yavchn.onList(setup);
})();
