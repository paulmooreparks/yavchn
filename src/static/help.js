(function () {
  var dialog = document.getElementById('help-dialog');
  var openBtn = document.querySelector('.help-button');
  if (!dialog || !openBtn) return;
  if (typeof dialog.showModal !== 'function') return; // no native <dialog> support

  function open() { if (!dialog.open) dialog.showModal(); }
  function close() { if (dialog.open) dialog.close(); }
  function toggle() { dialog.open ? close() : open(); }

  openBtn.addEventListener('click', open);

  var closeBtn = dialog.querySelector('.help-dialog-close');
  if (closeBtn) closeBtn.addEventListener('click', close);

  // Click on the backdrop (which is the dialog element itself, since the
  // content is inside a child) closes the dialog. The .help-dialog-frame
  // child captures clicks inside the visible content area.
  dialog.addEventListener('click', function (e) {
    if (e.target === dialog) close();
  });

  function inEditable(target) {
    if (!target) return false;
    var tag = (target.tagName || '').toLowerCase();
    if (tag === 'input' || tag === 'textarea' || tag === 'select') return true;
    return !!target.isContentEditable;
  }

  document.addEventListener('keydown', function (e) {
    if (e.ctrlKey || e.altKey || e.metaKey) return;
    if (e.key !== '?') return;
    if (inEditable(e.target)) return;
    e.preventDefault();
    toggle();
  });
})();
