(function () {
  function loadPane(pane, datasetKey, urlBuilder, fallbackSel) {
    if (!pane) return;
    var key = pane.dataset[datasetKey];
    if (!key) return;
    var body = pane.querySelector('.pane-body');
    if (!body) return;

    fetch(urlBuilder(key), { credentials: 'omit' })
      .then(function (r) { return r.text(); })
      .then(function (html) {
        if (html && html.length) {
          body.innerHTML = html;
          body.scrollTop = 0;
          // Let downstream listeners (e.g. discussion.js highlight-new)
          // know fresh content is in the pane.
          body.dispatchEvent(new CustomEvent('yavchn:loaded', { bubbles: true }));
        }
      })
      .catch(function () {
        var status = body.querySelector(fallbackSel);
        if (status) {
          status.textContent = 'Could not load. Use the "Open" link above.';
        }
      });
  }

  function loadArticlePane() {
    loadPane(
      document.querySelector('.pane-article'),
      'readerUrl',
      function (u) { return '/api/article?url=' + encodeURIComponent(u); },
      '.js-reader-status'
    );
  }

  function loadDiscussionPane() {
    var pane = document.querySelector('.pane-discussion');
    var source = pane ? (pane.dataset.discussionSource || 'hn') : 'hn';
    loadPane(
      pane,
      'discussionId',
      function (id) {
        return '/api/discussion?id=' + encodeURIComponent(id) +
               '&source=' + encodeURIComponent(source);
      },
      '.js-discussion-status'
    );
  }

  // Exposed so swap.js can re-trigger after swapping pane sections.
  window.yavchnLoadArticlePane = loadArticlePane;
  window.yavchnLoadDiscussionPane = loadDiscussionPane;

  // Manual refresh: bypasses the cache via /api/article?refresh=1 and
  // swaps the freshly-extracted content into the article pane. Document-
  // level listener so it survives pane-swap (the button lives inside the
  // .pane-article element that swap.js replaces).
  document.addEventListener('click', function (e) {
    var btn = e.target.closest('.article-refresh');
    if (!btn) return;
    e.preventDefault();
    var pane = document.querySelector('.pane-article');
    if (!pane) return;
    var url = pane.dataset.readerUrl;
    if (!url) return;
    var body = pane.querySelector('.pane-body');
    if (!body) return;
    btn.classList.add('spinning');
    btn.disabled = true;
    fetch('/api/article?refresh=1&url=' + encodeURIComponent(url), { credentials: 'omit' })
      .then(function (r) { return r.text(); })
      .then(function (html) {
        if (html && html.length) {
          body.innerHTML = html;
          body.scrollTop = 0;
          body.dispatchEvent(new CustomEvent('yavchn:loaded', { bubbles: true }));
        }
      })
      .catch(function () {
        var status = body.querySelector('.js-reader-status');
        if (status) status.textContent = 'Refresh failed. Try Open original.';
      })
      .finally(function () {
        btn.classList.remove('spinning');
        btn.disabled = false;
      });
  });

  loadArticlePane();
  loadDiscussionPane();
})();
