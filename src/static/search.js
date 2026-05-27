(function () {
  // When the search input is cleared while the visitor is already on a
  // /search results page, drop them back to the front page automatically.
  // Pure typing on / never triggers (we only navigate from /search).
  var input = document.querySelector('.search-bar input[type="search"]');
  if (!input) return;
  input.addEventListener('input', function () {
    if (input.value !== '') return;
    if (window.location.pathname.indexOf('/search') !== 0) return;
    window.location.href = '/';
  });
})();
