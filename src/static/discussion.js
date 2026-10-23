/* A story's discussion: collapsing threads, which is remembered per story,
   marking the comments that arrived since the reader's last visit, and the
   n, N and c keys, which act on the story in the window in front. */
(function () {
  'use strict';

  function storyOf(el) { return el.closest('.story'); }

  function loadCollapsed(storyID) {
    if (!storyID) return [];
    try {
      var arr = JSON.parse(localStorage.getItem('yavchn-collapsed:' + storyID) || '[]');
      return Array.isArray(arr) ? arr : [];
    } catch (err) { return []; }
  }

  function saveCollapsed(storyID, ids) {
    if (!storyID) return;
    // Cap so storage doesn't grow unboundedly on long busy threads.
    if (ids.length > 500) ids = ids.slice(-500);
    try { localStorage.setItem('yavchn-collapsed:' + storyID, JSON.stringify(ids)); } catch (err) {}
  }

  /* A comment is collapsed or expanded here alone, so its toggle, PUDL's
     raised card toggle, always says which with aria-expanded. */
  function setCollapsed(comment, on) {
    comment.classList.toggle('collapsed', on);
    var toggle = comment.querySelector(':scope > .comment-header > .comment-toggle');
    if (toggle) {
      toggle.setAttribute('aria-expanded', String(!on));
      toggle.setAttribute('aria-label', on ? 'Expand this comment' : 'Collapse this comment');
    }
  }

  // Shared by the comment's toggle, a click on its header and the c key,
  // so they can't drift.
  function toggleCollapse(comment) {
    if (!comment) return;
    var nowCollapsed = !comment.classList.contains('collapsed');
    setCollapsed(comment, nowCollapsed);
    var story = storyOf(comment);
    var storyID = story && story.dataset.storyId;
    var id = comment.dataset.id;
    if (!storyID || !id) return;
    var ids = loadCollapsed(storyID);
    var idx = ids.indexOf(id);
    if (nowCollapsed && idx < 0) ids.push(id);
    else if (!nowCollapsed && idx >= 0) ids.splice(idx, 1);
    saveCollapsed(storyID, ids);
  }

  document.addEventListener('click', function (e) {
    var toggle = e.target.closest('.story .comment-header > .comment-toggle');
    if (toggle) { toggleCollapse(toggle.closest('.comment')); return; }
    if (e.target.closest('a, button')) return;
    var header = e.target.closest('.story .comment-header');
    if (header) toggleCollapse(header.closest('.comment'));
  });

  function topLevel(story) {
    return Array.prototype.slice.call(story.querySelectorAll('.discussion-content > .thread > .comment'));
  }

  // The keyboard's place among a story's top-level comments is kept on the
  // story, so each window remembers its own. The story applet keeps it in
  // its state, and hears of each move through yavchn:comment-focus.
  function setFocus(story, idx, scroll) {
    if (scroll) story.dispatchEvent(new CustomEvent('yavchn:show-discussion'));
    var comments = topLevel(story);
    comments.forEach(function (c, i) { c.classList.toggle('focused', i === idx); });
    story.dataset.commentIdx = String(idx);
    var c = comments[idx];
    if (c && scroll) c.scrollIntoView({ block: 'start', behavior: 'smooth' });
    story.dispatchEvent(new CustomEvent('yavchn:comment-focus', { bubbles: true }));
    return c;
  }

  function currentIdx(story) { return parseInt(story.dataset.commentIdx || '-1', 10); }

  function step(story, dir) {
    var n = topLevel(story).length;
    if (!n) return false;
    var idx = currentIdx(story);
    setFocus(story, dir > 0 ? Math.min(idx + 1, n - 1) : Math.max(idx - 1, 0), true);
    return true;
  }

  function focusId(story, id, scroll) {
    var idx = topLevel(story).findIndex(function (c) { return c.dataset.id === id; });
    if (idx >= 0) setFocus(story, idx, scroll);
  }

  // The first top-level comment holding a comment new since the last visit.
  function firstNew(story) {
    var idx = topLevel(story).findIndex(function (c) {
      return c.classList.contains('comment-new') || !!c.querySelector('.comment-new');
    });
    if (idx < 0) return;
    var c = setFocus(story, idx, false);
    var target = c.classList.contains('comment-new') ? c : c.querySelector('.comment-new');
    if (c.classList.contains('collapsed')) toggleCollapse(c);
    target.scrollIntoView({ block: 'start', behavior: 'smooth' });
  }

  function collapseAll(story, on) {
    var storyID = story.dataset.storyId;
    var ids = loadCollapsed(storyID);
    topLevel(story).forEach(function (c) {
      setCollapsed(c, on);
      var i = ids.indexOf(c.dataset.id);
      if (on && i < 0 && c.dataset.id) ids.push(c.dataset.id);
      else if (!on && i >= 0) ids.splice(i, 1);
    });
    saveCollapsed(storyID, ids);
  }

  window.yavchn.discussion = { step: step, focusId: focusId, firstNew: firstNew, collapseAll: collapseAll };

  /* A comment's context menu: a right-click or a long press on a comment,
     its header's actions button, or Shift+F10 or the Menu key on its
     focused toggle, offers what the header's small
     controls do and a few things they can't. A link inside the comment's
     text, or text the reader has selected, keeps the browser's own menu,
     where open-in-new-tab and copy are expected. */
  var SOURCE_LABELS = { hn: 'Hacker News', lobsters: 'Lobsters' };

  function commentRows(comment) {
    var story = storyOf(comment);
    var lib = window.yavchn;
    var header = comment.querySelector(':scope > .comment-header');
    var author = header && header.querySelector('a.comment-author');
    var link = header && header.querySelector('a.comment-link');
    var text = comment.querySelector(':scope > .comment-text');
    var parent = comment.parentElement && comment.parentElement.closest('.comment');
    var source = SOURCE_LABELS[story && story.dataset.storySource];
    var collapsed = comment.classList.contains('collapsed');
    var rows = [
      { label: collapsed ? 'Expand this comment' : 'Collapse this comment', run: function () { toggleCollapse(comment); } }
    ];
    if (parent) rows.push({ label: 'Go to the parent comment', run: function () { parent.scrollIntoView({ block: 'start', behavior: 'smooth' }); } });
    if (story && topLevel(story).length > 1) {
      rows.push({ label: 'Collapse all threads', run: function () { collapseAll(story, true); } });
      rows.push({ label: 'Expand all threads', run: function () { collapseAll(story, false); } });
    }
    rows.push('-');
    if (link) rows.push({ label: 'Open on ' + (source || 'the source site'), glyph: 'open', run: function () { link.click(); } });
    if (author) rows.push({ label: 'Show ' + author.textContent + '’s profile', run: function () { author.click(); } });
    rows.push('-');
    if (link && lib.copy) rows.push({ label: 'Copy link to the comment', glyph: 'copy', run: function () { lib.copy(link.href); } });
    if (text && lib.copy) rows.push({ label: 'Copy the comment’s text', glyph: 'copy', run: function () { lib.copy(text.innerText.trim(), 'text'); } });
    return rows;
  }

  document.addEventListener('pudl:contextmenu', function (e) {
    var target = e.detail && e.detail.target;
    var comment = target && target.closest && target.closest('.story .comment[data-id]');
    if (!comment) return;
    if (target.closest('.comment-text a')) return;
    var sel = window.getSelection && window.getSelection();
    if (sel && !sel.isCollapsed && comment.contains(sel.anchorNode)) return;
    e.detail.mark(comment);
    e.detail.add(commentRows(comment));
  });

  document.addEventListener('keydown', function (e) {
    if (!window.yavchn.plainKey(e)) return;
    if (e.key !== 'n' && e.key !== 'N' && e.key !== 'c') return;
    var story = window.yavchn.currentStory();
    if (!story) return;
    if (e.key === 'c') {
      var comments = topLevel(story), idx = currentIdx(story);
      if (idx < 0 || idx >= comments.length) return;
      e.preventDefault();
      toggleCollapse(comments[idx]);
      return;
    }
    if (step(story, e.key === 'n' ? 1 : -1)) e.preventDefault();
  });

  document.addEventListener('yavchn:loaded', function (e) {
    var body = e.target;
    if (!body.classList.contains('story-discussion-body')) return;
    var story = storyOf(body);
    var storyID = story && story.dataset.storyId;
    if (!storyID) return;
    delete story.dataset.commentIdx;

    var collapsed = loadCollapsed(storyID);
    if (collapsed.length) {
      var set = {};
      collapsed.forEach(function (id) { set[id] = true; });
      body.querySelectorAll('.comment[data-id]').forEach(function (c) {
        if (set[c.dataset.id]) setCollapsed(c, true);
      });
    }

    // Mark comments newer than the last visit, then remember this one.
    var key = 'yavchn-last-visit:' + storyID;
    var prev = 0;
    try { prev = parseInt(localStorage.getItem(key) || '0', 10) || 0; } catch (err) {}
    if (prev > 0) {
      body.querySelectorAll('.comment[data-ts]').forEach(function (c) {
        if (parseInt(c.dataset.ts || '0', 10) > prev) c.classList.add('comment-new');
      });
    }
    try { localStorage.setItem(key, String(Math.floor(Date.now() / 1000))); } catch (err) {}
  });
})();
