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
    loadPane(
      document.querySelector('.pane-discussion'),
      'discussionId',
      function (id) { return '/api/discussion?id=' + encodeURIComponent(id); },
      '.js-discussion-status'
    );
  }

  // Exposed so swap.js can re-trigger after swapping pane sections.
  window.yavchnLoadArticlePane = loadArticlePane;
  window.yavchnLoadDiscussionPane = loadDiscussionPane;

  loadArticlePane();
  loadDiscussionPane();
})();
