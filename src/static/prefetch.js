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
    var a = row.querySelector('.meta .host');
    if (!a || !a.href) return '';
    var u;
    try { u = new URL(a.href); } catch (e) { return ''; }
    // Self-posts (Ask HN / Show HN text posts) link the host to HN itself;
    // there's no off-site article to prefetch.
    if (u.hostname === 'news.ycombinator.com') return '';
    if (u.protocol !== 'http:' && u.protocol !== 'https:') return '';
    return a.href;
  }

  function prefetch(url) {
    if (!url || prefetched[url]) return;
    if (count >= CAP) return;
    prefetched[url] = true;
    count++;
    // Fire-and-forget: the server's singleflight + SQLite cache do the real
    // work; the browser HTTP cache keeps the response warm for the click.
    try {
      fetch('/api/article?url=' + encodeURIComponent(url), {
        credentials: 'omit',
        // Low-priority hint so prefetches don't crowd out the active
        // pageview's requests on slow connections (Chromium / Safari).
        priority: 'low'
      }).catch(function () { /* swallow; click path retries */ });
    } catch (e) { /* old browser without priority */
      fetch('/api/article?url=' + encodeURIComponent(url), { credentials: 'omit' })
        .catch(function () {});
    }
  }

  var pending = null;
  var pendingRow = null;

  function clearPending() {
    if (pending) { clearTimeout(pending); pending = null; }
    pendingRow = null;
  }

  function onEnter(e) {
    var row = e.target.closest('.pane-list .story');
    if (!row) return;
    if (row === pendingRow) return;
    clearPending();
    var url = articleURLOf(row);
    if (!url) return;
    if (prefetched[url]) return;
    pendingRow = row;
    pending = setTimeout(function () {
      pending = null;
      pendingRow = null;
      prefetch(url);
    }, HOVER_MS);
  }

  function onLeave(e) {
    if (!e.target.closest) return;
    if (!pendingRow) return;
    // Only clear if leaving the row we armed; mousemove inside the row
    // dispatches mouseout for children too.
    if (e.target === pendingRow || (pendingRow.contains && pendingRow.contains(e.target))) {
      var to = e.relatedTarget;
      if (to && pendingRow.contains(to)) return;
      clearPending();
    }
  }

  if (saveData()) return;

  document.addEventListener('mouseover', onEnter);
  document.addEventListener('mouseout', onLeave);
})();
