(function () {
  var pane = document.querySelector('.pane-list');
  if (!pane) return;
  var ul = pane.querySelector('.stories');
  if (!ul) return;

  // Derive the base path for the current source-list.
  // Strip a trailing /s/{id} so /show/s/123 becomes /show/, /s/123 becomes /.
  var basePath = window.location.pathname.replace(/\/s\/\d+\/?$/, '/');
  if (!basePath.endsWith('/')) basePath = basePath + '/';

  var params = new URLSearchParams(window.location.search);
  var currentPage = parseInt(params.get('page') || '1', 10);
  if (isNaN(currentPage) || currentPage < 1) currentPage = 1;

  var hasNext = ul.dataset.hasNext === 'true';
  var inFlight = false;
  var pager = pane.querySelector('.pager');

  function nearBottom() {
    return pane.scrollTop + pane.clientHeight >= pane.scrollHeight - 200;
  }

  function markEnd() {
    if (pager) pager.style.display = 'none';
    if (!pane.querySelector('.end-of-list')) {
      var div = document.createElement('div');
      div.className = 'end-of-list';
      div.textContent = 'End of list';
      ul.insertAdjacentElement('afterend', div);
    }
  }

  function loadMore() {
    if (inFlight || !hasNext) return;
    inFlight = true;
    var nextPage = currentPage + 1;
    var url = basePath + '?page=' + nextPage;

    fetch(url, { credentials: 'omit', headers: { 'Accept': 'text/html' } })
      .then(function (r) {
        if (!r.ok) throw new Error('upstream ' + r.status);
        return r.text();
      })
      .then(function (html) {
        var doc = new DOMParser().parseFromString(html, 'text/html');
        var newUL = doc.querySelector('.stories');
        if (!newUL) {
          hasNext = false;
          markEnd();
          return;
        }
        var newStories = newUL.querySelectorAll('.story');
        if (!newStories.length) {
          hasNext = false;
          markEnd();
          return;
        }
        Array.prototype.forEach.call(newStories, function (li) {
          ul.appendChild(li);
        });
        currentPage = nextPage;
        hasNext = newUL.dataset.hasNext === 'true';
        if (!hasNext) markEnd();
      })
      .catch(function () {
        // Leave hasNext true so the user can try again by scrolling.
      })
      .finally(function () {
        inFlight = false;
        // If the viewport still isn't full, keep loading.
        if (hasNext && nearBottom()) loadMore();
      });
  }

  pane.addEventListener('scroll', function () {
    if (nearBottom()) loadMore();
  }, { passive: true });

  // Some viewports + short lists may not need scrolling to reach the bottom.
  if (hasNext && nearBottom()) loadMore();
})();
