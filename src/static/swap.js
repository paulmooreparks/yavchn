(function () {
  var list = document.querySelector('.pane-list');
  if (!list) return;

  function performSwap(url, pushHistory) {
    fetch(url, { credentials: 'omit', headers: { 'Accept': 'text/html' } })
      .then(function (r) { return r.text(); })
      .then(function (html) {
        var doc = new DOMParser().parseFromString(html, 'text/html');
        var newArticle = doc.querySelector('.pane-article');
        var newDiscussion = doc.querySelector('.pane-discussion');
        var oldArticle = document.querySelector('.pane-article');
        var oldDiscussion = document.querySelector('.pane-discussion');

        if (newArticle && oldArticle) oldArticle.replaceWith(newArticle);
        if (newDiscussion && oldDiscussion) oldDiscussion.replaceWith(newDiscussion);

        var titleEl = doc.querySelector('title');
        if (titleEl && titleEl.textContent) document.title = titleEl.textContent;

        var match = url.match(/\/s\/([a-z0-9]+)/i);
        if (match) {
          var selectedID = match[1];
          var prevSelected = list.querySelectorAll('.story.selected');
          for (var i = 0; i < prevSelected.length; i++) {
            prevSelected[i].classList.remove('selected');
          }
          var endsWith = '/s/' + selectedID;
          var anchors = list.querySelectorAll('.story .title a');
          for (var j = 0; j < anchors.length; j++) {
            var href = anchors[j].getAttribute('href') || '';
            if (href === endsWith || href.endsWith(endsWith)) {
              var li = anchors[j].closest('.story');
              if (li) li.classList.add('selected');
            }
          }
        } else {
          // No /s/{id} -- clear selection (e.g. swap to /, /show/, etc.).
          var sel = list.querySelectorAll('.story.selected');
          for (var k = 0; k < sel.length; k++) sel[k].classList.remove('selected');
        }

        if (pushHistory) {
          history.pushState({}, '', url);
        }

        if (window.yavchnLoadArticlePane) window.yavchnLoadArticlePane();
        if (window.yavchnLoadDiscussionPane) window.yavchnLoadDiscussionPane();
      })
      .catch(function () {
        // Fall back to a real navigation if anything goes wrong.
        window.location.href = url;
      });
  }

  list.addEventListener('click', function (e) {
    if (e.ctrlKey || e.metaKey || e.shiftKey || e.altKey) return;
    if (e.button !== 0) return;
    var a = e.target.closest('.story .title a');
    if (!a) return;
    var href = a.getAttribute('href');
    if (!href) return;
    // Only swap for our own /{source}/[tab/]s/{id} URLs (HN uses digits,
    // Lobsters uses base36 short_ids -- e.g. /lobsters/s/abc123).
    if (!/\/s\/[a-z0-9]+($|\?)/i.test(href)) return;
    e.preventDefault();
    performSwap(href, true);
  });

  window.addEventListener('popstate', function () {
    performSwap(window.location.pathname + window.location.search, false);
  });
})();
