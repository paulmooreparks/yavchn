(function () {
  var pane = document.querySelector('.pane-article');
  if (!pane) return;
  var url = pane.dataset.readerUrl;
  if (!url) return;
  var body = pane.querySelector('.pane-body');
  if (!body) return;

  fetch('/api/article?url=' + encodeURIComponent(url), { credentials: 'omit' })
    .then(function (r) {
      if (!r.ok) throw new Error('reader unavailable');
      return r.text();
    })
    .then(function (html) {
      body.innerHTML = html;
      body.scrollTop = 0;
    })
    .catch(function () {
      var status = body.querySelector('.js-reader-status');
      if (status) {
        status.textContent = 'Reader-mode could not extract this page. Use "Open article" above.';
      }
    });
})();
