(function () {
  function lazyLoad(paneSel, datasetKey, urlBuilder, fallbackSel) {
    var pane = document.querySelector(paneSel);
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

  lazyLoad(
    '.pane-article',
    'readerUrl',
    function (u) { return '/api/article?url=' + encodeURIComponent(u); },
    '.js-reader-status'
  );

  lazyLoad(
    '.pane-discussion',
    'discussionId',
    function (id) { return '/api/discussion?id=' + encodeURIComponent(id); },
    '.js-discussion-status'
  );
})();
