(function () {
  // Cardinality guard: even with dedup, a scan of the whole list shouldn't
  // fan out hundreds of requests. Cap how many distinct articles we'll
  // prefetch per page-view.
  var CAP = 30;
  var HOVER_MS = 500;
  var prefetched = {};
  var count = 0;

  // Respect the user's data-saving preference (slow networks, metered
  // connections). The cost of a missed prefetch is a few hundred ms; the
  // cost of unwanted bytes on a metered link is real money.
  function saveData() {
    var c = navigator.connection;
    return !!(c && c.saveData);
  }

  function articleURLOf(row) {
    var u;
    try { u = new URL(row.dataset.url || ''); } catch (e) { return ''; }
    // Text posts link to the source's own site; there's no off-site
    // article to prefetch.
    if (u.hostname === 'news.ycombinator.com' || u.hostname === 'lobste.rs') return '';
    if (u.protocol !== 'http:' && u.protocol !== 'https:') return '';
    return u.href;
  }

  function prefetch(url) {
    if (!url || prefetched[url]) return;
    if (count >= CAP) return;
    prefetched[url] = true;
    count++;
    // Fire-and-forget: the server's singleflight + SQLite cache do the real
    // work; the browser HTTP cache keeps the response warm for the click.
    // The low priority keeps prefetches from crowding out the reader's own
    // requests on a slow connection.
    fetch('/api/article?url=' + encodeURIComponent(url), { credentials: 'omit', priority: 'low' })
      .catch(function () { /* swallow; the window's own request retries */ });
  }

  var pending = null;
  var pendingRow = null;

  function clearPending() {
    if (pending) { clearTimeout(pending); pending = null; }
    pendingRow = null;
  }

  function onEnter(e) {
    var row = e.target.closest && e.target.closest('.story-list .story-row');
    if (!row || row === pendingRow) return;
    clearPending();
    var url = articleURLOf(row);
    if (!url || prefetched[url]) return;
    pendingRow = row;
    pending = setTimeout(function () {
      pending = null;
      pendingRow = null;
      prefetch(url);
    }, HOVER_MS);
  }

  function onLeave(e) {
    if (!pendingRow) return;
    // Only clear if leaving the row we armed; mousemove inside the row
    // dispatches mouseout for children too.
    if (e.target === pendingRow || pendingRow.contains(e.target)) {
      var to = e.relatedTarget;
      if (to && pendingRow.contains(to)) return;
      clearPending();
    }
  }

  if (saveData()) return;

  document.addEventListener('mouseover', onEnter);
  document.addEventListener('mouseout', onLeave);
})();
