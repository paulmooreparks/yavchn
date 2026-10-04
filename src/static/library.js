/* Collections and notes: the reader's own lists of stories and private
   notes on them. Both live in browser storage, as pins do, under
   yavchn-collections, yavchn-collected and yavchn-notes, and accounts.js
   synchronizes them when the reader is signed in.

   Shapes:
     yavchn-collections  { "<id>": { name, created_at } }
     yavchn-collected    { "<id>:<source>-<story>": { source, id, title, url, host, by, score, comments, added_at } }
     yavchn-notes        { "<source>-<story>": { source, id, title, url, host, by, score, comments, text, updated_at } }

   This script fills the Collections and Notes views, the Collections
   view's collection menu, the note panel in every story, the
   note marks on story rows, and the collection and note commands of the
   story applet's Story menu (library.storyMenu). */
(function () {
  'use strict';
  var lib = window.yavchn, stored = lib.stored, esc = stored.esc;
  var COLLECTIONS = 'yavchn-collections', COLLECTED = 'yavchn-collected', NOTES = 'yavchn-notes';
  var MAX_COLLECTIONS = 100, MAX_COLLECTED = 2000, MAX_NOTES = 1000, MAX_NAME = 100;

  function read(key) {
    try {
      var v = JSON.parse(window.yavchnStorage.getItem(key) || '{}');
      return v && typeof v === 'object' && !Array.isArray(v) ? v : {};
    } catch (e) { return {}; }
  }
  function write(key, value) {
    try { window.yavchnStorage.setItem(key, JSON.stringify(value)); return true; } catch (e) { return false; }
  }
  function now() { return Math.floor(Date.now() / 1000); }
  function newID() {
    var bytes = new Uint8Array(12), out = '';
    crypto.getRandomValues(bytes);
    bytes.forEach(function (b) { out += 'abcdefghijklmnopqrstuvwxyz0123456789'.charAt(b % 36); });
    return out;
  }
  function has(map, key) { return Object.prototype.hasOwnProperty.call(map, key); }

  /* A story's listing from its row or its story root, with its id. */
  function storyOf(el) {
    var d = el.dataset;
    var link = el.querySelector && el.querySelector(':scope > .md-item');
    var title = d.title || (link && link.firstChild ? link.firstChild.textContent.trim() : '');
    return Object.assign(stored.metaOf(el, title), { id: d.storyId || d.id, source: d.storySource || d.source || 'hn' });
  }
  function keyOf(story) { return story.source + '-' + story.id; }

  /* === Collections ====================================================== */
  function collections() {
    var map = read(COLLECTIONS);
    return Object.keys(map).map(function (id) { return { id: id, name: map[id].name, created_at: map[id].created_at }; })
      .sort(function (a, b) { return a.name.localeCompare(b.name, undefined, { sensitivity: 'base' }); });
  }
  function nameProblem(name, except) {
    if (!name) return 'Enter a name for the collection.';
    if (name.length > MAX_NAME) return 'Use ' + MAX_NAME + ' characters or fewer.';
    var lower = name.toLocaleLowerCase();
    if (collections().some(function (c) { return c.id !== except && c.name.toLocaleLowerCase() === lower; })) {
      return 'You already have a collection with this name.';
    }
    return '';
  }
  function create(name) {
    var map = read(COLLECTIONS);
    if (Object.keys(map).length >= MAX_COLLECTIONS) return { error: 'You have ' + MAX_COLLECTIONS + ' collections, which is the limit. Delete one to make another.' };
    var problem = nameProblem(name, '');
    if (problem) return { error: problem };
    var id = newID();
    map[id] = { name: name, created_at: now() };
    return write(COLLECTIONS, map) ? { id: id } : { error: 'Browser storage is full or unavailable.' };
  }
  function rename(id, name) {
    var map = read(COLLECTIONS);
    if (!has(map, id)) return { error: 'This collection no longer exists.' };
    var problem = nameProblem(name, id);
    if (problem) return { error: problem };
    map[id].name = name;
    return write(COLLECTIONS, map) ? { id: id } : { error: 'Browser storage is full or unavailable.' };
  }
  function remove(id) {
    var map = read(COLLECTIONS), entries = read(COLLECTED);
    delete map[id];
    Object.keys(entries).forEach(function (key) { if (key.split(':')[0] === id) delete entries[key]; });
    // Entries go first, so no write ever leaves an entry without its collection.
    write(COLLECTED, entries);
    write(COLLECTIONS, map);
  }
  function holds(id, story) { return has(read(COLLECTED), id + ':' + keyOf(story)); }
  function setHeld(id, story, on) {
    var entries = read(COLLECTED), key = id + ':' + keyOf(story);
    if (on) {
      if (!has(read(COLLECTIONS), id)) return false;
      if (!has(entries, key) && Object.keys(entries).length >= MAX_COLLECTED) {
        lib.libraryNotice('Your collections hold ' + MAX_COLLECTED + ' stories, which is the limit. Remove some to add more.');
        return false;
      }
      entries[key] = Object.assign({}, story, { added_at: now() });
    } else delete entries[key];
    return write(COLLECTED, entries);
  }

  /* === Notes ============================================================ */
  function noteOf(story) { var n = read(NOTES)[keyOf(story)]; return n ? n.text : ''; }
  function saveNote(story, text) {
    var notes = read(NOTES), key = keyOf(story);
    if (!text.trim()) {
      if (!has(notes, key)) return 'none';
      delete notes[key];
      return write(NOTES, notes) ? 'deleted' : 'failed';
    }
    if (!has(notes, key) && Object.keys(notes).length >= MAX_NOTES) return 'full';
    notes[key] = Object.assign({}, story, { text: text, updated_at: now() });
    return write(NOTES, notes) ? 'saved' : 'failed';
  }

  function notice(message) {
    if (window.pudlToast) window.pudlToast(message);
  }
  lib.libraryNotice = notice;

  /* === Dialogs ========================================================== */
  var dialog = document.getElementById('collection-dialog');
  var pending = null; // { mode: 'new' | 'rename', id, story }
  function openDialog(mode, id, story) {
    if (!dialog) return;
    pending = { mode: mode, id: id, story: story };
    var input = dialog.querySelector('#collection-name');
    var current = mode === 'rename' ? read(COLLECTIONS)[id] : null;
    dialog.querySelector('.dialog-title').textContent = mode === 'rename' ? 'Rename the collection' : 'New collection';
    dialog.querySelector('[data-collection-submit]').textContent = mode === 'rename' ? 'Rename' : story ? 'Create and add the story' : 'Create';
    input.value = current ? current.name : '';
    showError('');
    dialog.showModal();
    input.select();
  }
  function showError(message) {
    var error = dialog.querySelector('[data-collection-error]');
    var input = dialog.querySelector('#collection-name');
    error.textContent = message;
    error.hidden = !message;
    if (message) input.setAttribute('aria-invalid', 'true'); else input.removeAttribute('aria-invalid');
  }
  if (dialog) dialog.querySelector('[data-collection-form]').addEventListener('submit', function (e) {
    e.preventDefault();
    if (!pending) return;
    var name = dialog.querySelector('#collection-name').value.trim();
    var result = pending.mode === 'rename' ? rename(pending.id, name) : create(name);
    if (result.error) { showError(result.error); dialog.querySelector('#collection-name').focus(); return; }
    if (pending.story) setHeld(result.id, pending.story, true);
    var created = pending.mode === 'new' && !pending.story ? result.id : '';
    pending = null;
    dialog.close();
    if (created) go(created);
    else refresh();
  });

  var deleting = null;
  var deleteDialog = document.getElementById('collection-delete-dialog');
  function confirmDelete(id) {
    var c = read(COLLECTIONS)[id];
    if (!c || !deleteDialog) return;
    var count = Object.keys(read(COLLECTED)).filter(function (k) { return k.split(':')[0] === id; }).length;
    deleteDialog.querySelector('[data-collection-delete-body]').textContent = 'This deletes “' + c.name + '” and its list of ' +
      count + (count === 1 ? ' story' : ' stories') + '. The stories stay in your other collections, your pins, and their feeds.';
    deleting = id;
    deleteDialog.returnValue = '';
    deleteDialog.showModal();
  }
  if (deleteDialog) deleteDialog.addEventListener('close', function () {
    if (deleteDialog.returnValue === 'delete' && deleting) { remove(deleting); go(''); }
    deleting = null;
  });

  /* The collections the Collections view's address chooses, and the one it
     shows alone, which the collection menu's commands and the rows' remove buttons act on. */
  function chosen(view) { return (view && view.dataset.collections || '').split(' ').filter(Boolean); }
  function single(view) { var c = chosen(view); return c.length === 1 ? c[0] : ''; }

  /* Move the Collections view to one collection, or to all of them, keeping
     the other filters and the windows in the address. */
  function go(id) {
    var url = new URL(location.href);
    url.pathname = '/collections/';
    url.searchParams.delete('c');
    if (id) url.searchParams.append('c', id);
    var link = document.createElement('a');
    link.href = url.pathname + url.search;
    link.setAttribute('data-region-link', '');
    // pudl-regions.js swaps the list in place for a link it owns.
    document.body.appendChild(link);
    link.click();
    link.remove();
  }

  document.addEventListener('click', function (e) {
    var action = e.target.closest && e.target.closest('[data-collection-action]');
    if (action) {
      var id = single(collectionsView());
      var panel = action.closest('[popover]');
      if (panel && panel.hidePopover) panel.hidePopover();
      if (action.dataset.collectionAction === 'new') openDialog('new', '', null);
      else if (action.dataset.collectionAction === 'rename' && id) openDialog('rename', id, null);
      else if (action.dataset.collectionAction === 'delete' && id) confirmDelete(id);
      return;
    }
    var uncollect = e.target.closest && e.target.closest('.story-row .story-uncollect');
    if (uncollect) {
      e.preventDefault();
      var one = single(collectionsView());
      if (one) setHeld(one, storyOf(uncollect.closest('.story-row')), false);
      refresh();
    }
  });

  /* === The Collections and Notes views ================================== */
  function collectionsView() { return document.querySelector('.story-list[data-source="collections"]'); }
  function notesView() { return document.querySelector('.story-list[data-source="notes"]'); }

  /* The Collections menu's checkboxes, which belong to the filter form as
     the Show menu's do, and the chips' names, which only the browser knows. */
  function renderChooser(view) {
    var panel = document.querySelector('[data-collection-choice]');
    var picked = chosen(view), all = read(COLLECTIONS);
    if (panel) {
      var list = collections();
      panel.innerHTML = list.length ? list.map(function (c) {
        return '<label class="check menu-check"><input type="checkbox" form="pin-filter" name="c" value="' + esc(c.id) + '"' +
          (picked.indexOf(c.id) >= 0 ? ' checked' : '') + '> ' + esc(c.name) + '</label>';
      }).join('') : '<p class="menu-empty">No collections yet.</p>';
    }
    var label = document.querySelector('[data-collection-menu-label]');
    if (label && picked.length === 1) label.textContent = all[picked[0]] ? all[picked[0]].name : 'A missing collection';
    document.querySelectorAll('[data-collection-chip]').forEach(function (chip) {
      var c = all[chip.dataset.collectionChip];
      var name = c ? c.name : 'A collection not in this browser';
      chip.querySelector('.filter-chip-label').textContent = name;
      chip.querySelector('.filter-chip-x').setAttribute('aria-label', 'Remove the filter ' + name);
    });
  }

  function renderCollections() {
    var view = collectionsView();
    if (!view) return;
    renderChooser(view);
    var all = read(COLLECTIONS), entries = read(COLLECTED);
    var picked = chosen(view).filter(function (c) { return has(all, c); });
    var id = picked.length === 1 && chosen(view).length === 1 ? picked[0] : '';
    if (chosen(view).length && !picked.length) {
      var box = view.querySelector('.stories');
      var every = new URL(location.href);
      every.searchParams.delete('c');
      if (box) box.innerHTML = '<div class="empty-state list-empty"><p class="empty-state-title">' +
        (chosen(view).length === 1 ? 'This collection is not here' : 'These collections are not here') + '</p>' +
        '<p class="empty-state-body">They may have been deleted, or they belong to an account that is not signed in on this browser.</p>' +
        '<div class="empty-state-actions"><a class="btn btn-sm" href="' + esc(every.pathname + every.search) + '">Show every collection</a></div></div>';
      return;
    }
    document.title = (id ? all[id].name : 'Collections') + ' · YAVCHN';
    var byStory = {};
    Object.keys(entries).forEach(function (key) {
      var cid = key.split(':')[0], e = entries[key];
      if ((picked.length && picked.indexOf(cid) < 0) || !has(all, cid)) return;
      var s = byStory[e.source + '-' + e.id];
      if (!s) s = byStory[e.source + '-' + e.id] = Object.assign({}, e, { at: e.added_at, names: [] });
      s.at = Math.max(s.at || 0, e.added_at || 0);
      s.names.push(all[cid].name);
    });
    var list = Object.keys(byStory).map(function (k) { return byStory[k]; });
    var none = !Object.keys(all).length;
    stored.render(view, list, {
      noun: 'collected stories', time: 'added',
      emptyTitle: none ? 'No collections yet' : id ? 'This collection is empty' : 'Your collections are empty',
      emptyBody: none ? 'Choose New collection in the collection menu, or use Story > Add to collection in a story’s window.' :
        'Use Story > Add to collection in a story’s window to file it here.',
      remove: id ? 'Remove from this collection' : '',
      extra: id ? null : function (s) { return '<span class="md-meta collection-names">In ' + esc(s.names.sort().join(', ')) + '</span>'; }
    });
  }

  function excerpt(text) {
    var line = text.replace(/\s+/g, ' ').trim();
    return line.length > 160 ? line.slice(0, 159) + '…' : line;
  }
  function renderNotes() {
    var view = notesView();
    if (!view) return;
    var notes = read(NOTES);
    stored.render(view, Object.keys(notes).map(function (k) { return Object.assign({}, notes[k], { at: notes[k].updated_at }); }), {
      noun: 'notes', time: 'edited',
      emptyTitle: 'No notes yet',
      emptyBody: 'Choose Add note in a story’s toolbar to write one. Only you can see your notes.',
      text: function (s) { return s.text; },
      extra: function (s) { return '<span class="md-meta note-excerpt">' + esc(excerpt(s.text)) + '</span>'; }
    });
  }

  /* Rows in every feed show a note's mark, a glyph with words for a reader
     who cannot see it. */
  function markRows() {
    var notes = read(NOTES);
    document.querySelectorAll('.story-row[data-id]').forEach(function (row) {
      var on = has(notes, (row.dataset.source || 'hn') + '-' + row.dataset.id);
      row.classList.toggle('has-note', on);
      var meta = row.querySelector('.md-item > .md-meta');
      var mark = meta && meta.querySelector('.note-mark');
      if (on && meta && !mark) {
        mark = document.createElement('span');
        mark.className = 'note-mark';
        mark.setAttribute('role', 'img');
        mark.setAttribute('aria-label', 'Has a note');
        mark.title = 'Has a note';
        meta.insertBefore(mark, meta.firstChild);
      } else if (!on && mark) mark.remove();
    });
  }

  /* === The note panel in a story ======================================== */
  var panels = 0, timers = new WeakMap();
  function storyRoot(el) { return el.closest('.story[data-story-key]'); }
  function syncToggle(root, open) {
    var toggle = root.querySelector('[data-note-toggle]');
    if (!toggle) return;
    var text = noteOf(storyOf(root));
    toggle.hidden = false;
    toggle.setAttribute('aria-expanded', String(open));
    toggle.classList.toggle('has-note', !!text);
    toggle.querySelector('.story-note-label').textContent = text ? 'Note' : 'Add note';
    toggle.title = open ? 'Hide your note' : text ? 'Show your note' : 'Write a note on this story';
  }
  function setOpen(root, open, focus) {
    var panel = root.querySelector('[data-note-panel]');
    if (!panel) return;
    panel.hidden = !open;
    syncToggle(root, open);
    if (open && focus) panel.querySelector('[data-note-text]').focus();
    if (window.pudlSplit) window.pudlSplit.refresh();
  }
  /* The reader opening or closing the panel is remembered with the story's
     other state (story.js), so a reload or the story opened again finds the
     panel as it was left. */
  function choose(root, open, focus) {
    var panel = root.querySelector('[data-note-panel]');
    if (!panel) return;
    panel.dataset.noteChosen = 'true';
    setOpen(root, open, focus);
    root.dispatchEvent(new CustomEvent('yavchn:note-toggle', { bubbles: true }));
  }
  /* Fill each story's panel for the story it now shows. The panel is as the
     reader left it on this story, and otherwise opens by itself when the
     story has a note, so the reader sees why they kept it. */
  function bindPanels() {
    document.querySelectorAll('.story[data-story-key] [data-note-panel]').forEach(function (panel) {
      var root = storyRoot(panel), key = root.dataset.storyKey;
      var area = panel.querySelector('[data-note-text]');
      if (panel.dataset.noteFor !== key) {
        // Window and page markup can repeat a story, so each panel gets its own ids.
        var n = ++panels;
        panel.id = 'note-panel-' + n;
        area.id = 'note-text-' + n;
        panel.querySelector('label').htmlFor = area.id;
        var toggle = root.querySelector('[data-note-toggle]');
        if (toggle) toggle.setAttribute('aria-controls', panel.id);
        panel.dataset.noteFor = key;
        delete panel.dataset.noteApplied;
        area.value = noteOf(storyOf(root));
        setOpen(root, !!area.value, false);
      }
      // story.js gives the remembered choice as its state comes in, which
      // can be after the panel was first filled.
      var wanted = root.dataset.noteWanted || '';
      if (wanted && panel.dataset.noteApplied !== wanted) {
        panel.dataset.noteApplied = wanted;
        panel.dataset.noteChosen = 'true';
        setOpen(root, wanted === 'open', false);
      } else if (document.activeElement !== area && !timers.get(area)) {
        // Another tab or device changed the note while this one was idle.
        area.value = noteOf(storyOf(root));
        syncToggle(root, !panel.hidden);
      }
    });
  }
  function status(panel, text) { panel.querySelector('[data-note-status]').textContent = text; }
  function commit(area) {
    clearTimeout(timers.get(area));
    timers.delete(area);
    var root = storyRoot(area), panel = area.closest('[data-note-panel]');
    if (!root) return;
    var result = saveNote(storyOf(root), area.value);
    if (result === 'saved') status(panel, 'Saved in this browser' + (document.querySelector('meta[name="yavchn-account"]') ? ' and your account.' : '.'));
    else if (result === 'deleted') status(panel, 'Note deleted.');
    else if (result === 'full') status(panel, 'You have ' + MAX_NOTES + ' notes, which is the limit. Delete one to write another.');
    else if (result === 'failed') status(panel, 'Your note could not be saved. Browser storage may be full.');
    syncToggle(root, !panel.hidden);
    markRows();
    if (notesView()) renderNotes();
  }
  document.addEventListener('click', function (e) {
    var toggle = e.target.closest && e.target.closest('[data-note-toggle]');
    if (!toggle) return;
    var root = storyRoot(toggle);
    var panel = root && root.querySelector('[data-note-panel]');
    if (panel) choose(root, panel.hidden, panel.hidden);
  });
  document.addEventListener('input', function (e) {
    if (!e.target.matches || !e.target.matches('[data-note-text]')) return;
    var area = e.target;
    clearTimeout(timers.get(area));
    status(area.closest('[data-note-panel]'), 'Saving…');
    timers.set(area, setTimeout(function () { commit(area); }, 600));
  });
  document.addEventListener('focusout', function (e) {
    if (e.target.matches && e.target.matches('[data-note-text]') && timers.get(e.target)) commit(e.target);
  });
  // A note typed just before the page goes away is saved, not lost.
  window.addEventListener('pagehide', function () {
    document.querySelectorAll('[data-note-text]').forEach(function (area) { if (timers.get(area)) commit(area); });
  });

  /* === The Story menu =================================================== */
  function storyMenu(root) {
    var story = storyOf(root);
    var items = collections().map(function (c) {
      return { label: c.name, checked: holds(c.id, story), run: function () { setHeld(c.id, story, !holds(c.id, story)); refresh(); } };
    });
    if (items.length) items.push('-');
    items.push({ label: 'New collection…', run: function () { openDialog('new', '', story); } });
    var panel = root.querySelector('[data-note-panel]');
    var text = noteOf(story);
    return [
      { label: 'Add to collection', items: items },
      { label: text ? (panel && !panel.hidden ? 'Hide the note' : 'Show the note') : 'Add a note…', disabled: !panel,
        run: function () { choose(root, panel.hidden, panel.hidden); } }
    ];
  }

  function refresh() {
    renderCollections();
    renderNotes();
    markRows();
    bindPanels();
  }
  lib.library = { storyMenu: storyMenu, refresh: refresh, bindNotes: bindPanels };

  /* A filter menu's checkbox applies at once, as on parkscomputing.com: it
     submits the filter form, which pudl-regions.js turns into a swap of the
     list bar. The menu that was open opens again on the new bar with focus
     on the same box, so several boxes can be ticked in a row. */
  var reopen = null;
  document.addEventListener('change', function (e) {
    var panel = e.target.closest && e.target.closest('[data-filter-menu]');
    if (!panel || !e.target.form) return;
    reopen = { panel: panel.id, name: e.target.name, value: e.target.value };
    e.target.form.requestSubmit();
  });
  document.addEventListener('pudl:regions-swap', function () {
    var was = reopen;
    reopen = null;
    // After every swap listener, library.js's own included, has rebuilt the bar.
    if (was) requestAnimationFrame(function () {
      var panel = document.getElementById(was.panel);
      if (!panel || !panel.showPopover) return;
      panel.showPopover();
      var box = panel.querySelector('input[name="' + CSS.escape(was.name) + '"][value="' + CSS.escape(was.value) + '"]');
      if (box) box.focus();
    });
  });

  // The words filter narrows these lists as the reader types, as in Pinned.
  var typing = 0;
  document.addEventListener('input', function (e) {
    if (!e.target.matches || !e.target.matches('.pin-filter input[name="q"]')) return;
    clearTimeout(typing);
    typing = setTimeout(function () { renderCollections(); renderNotes(); }, 120);
  });

  lib.onList(refresh);
  document.addEventListener('yavchn:rows-appended', markRows);
  document.addEventListener('yavchn:story-change', bindPanels);
  document.addEventListener('pudl:window-open', bindPanels);
  document.addEventListener('pudl:windows-change', bindPanels);
  window.addEventListener('storage', function (e) {
    if (e.key === COLLECTIONS || e.key === COLLECTED || e.key === NOTES) refresh();
  });
  // Story applets mount as their scripts load; catch any that mounted after this ran.
  document.addEventListener('DOMContentLoaded', bindPanels);
})();
