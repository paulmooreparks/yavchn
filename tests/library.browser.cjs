const assert = require('node:assert/strict');
const { chromium } = require(process.env.YAVCHN_PLAYWRIGHT || 'playwright');

(async function () {
  const origin = process.argv[2];
  const browser = await chromium.launch({ channel: process.env.YAVCHN_BROWSER_CHANNEL || 'msedge', headless: true });
  try {
    const page = await browser.newPage({ viewport: { width: 1500, height: 1000 } });
    page.setDefaultTimeout(8000);
    const errors = [];
    page.on('pageerror', e => errors.push(e.message));
    page.on('console', msg => { if (msg.type() === 'warning' && msg.text().includes('pudl-menubar')) errors.push(msg.text()); });
    await page.route('**/api/article?*', route => route.fulfill({ contentType: 'text/html', body: '<article>Article fixture</article>' }));
    await page.route('**/api/discussion?*', route => route.fulfill({ contentType: 'text/html', body: '<div class="discussion-content">Discussion fixture</div>' }));
    const stored = key => page.evaluate(k => JSON.parse(localStorage.getItem(k) || '{}'), key);
    const storyMenu = async () => {
      await page.locator('.menubar-title').filter({ hasText: /^Story$/ }).click();
      await page.getByRole('menuitem', { name: 'Add to collection', exact: true }).click();
    };

    // File a story into a new collection from its Story menu.
    await page.goto(origin + '/hn/?view=window');
    await page.locator('#row-hn-1 > a.md-item').click();
    await page.waitForFunction(() => document.querySelector('.win.active .story')?.dataset.storyKey === 'hn-1');
    await storyMenu();
    await page.getByRole('menuitem', { name: 'New collection…', exact: true }).click();
    const dialog = page.locator('#collection-dialog');
    await dialog.getByLabel('Name').fill('Reading list');
    await dialog.getByRole('button', { name: 'Create and add the story', exact: true }).click();
    await dialog.waitFor({ state: 'hidden' });
    const [id] = Object.keys(await stored('yavchn-collections'));
    assert.match(id, /^[a-z0-9]{12}$/);
    assert.equal((await stored('yavchn-collected'))[id + ':hn-1'].title, 'First <story>');
    await storyMenu();
    assert.equal(await page.getByRole('menuitemcheckbox', { name: 'Reading list', exact: true }).getAttribute('aria-checked'), 'true');
    await page.keyboard.press('Escape');
    await page.keyboard.press('Escape');

    // A duplicate name is refused in the dialog rather than created.
    await storyMenu();
    await page.getByRole('menuitem', { name: 'New collection…', exact: true }).click();
    await dialog.getByLabel('Name').fill('reading LIST');
    await dialog.getByRole('button', { name: 'Create and add the story', exact: true }).click();
    assert.equal(await dialog.locator('[data-collection-error]').textContent(), 'You already have a collection with this name.');
    assert.equal(await dialog.getByLabel('Name').getAttribute('aria-invalid'), 'true');
    await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
    assert.equal(Object.keys(await stored('yavchn-collections')).length, 1);

    // A note saves as the reader types and marks the story's row.
    const reader = page.locator('.win.active .story');
    await reader.getByRole('button', { name: 'Add note', exact: true }).click();
    await reader.getByLabel('Your note on this story').fill('Remember the benchmark table.');
    await reader.locator('[data-note-status]').filter({ hasText: /^Saved/ }).waitFor();
    assert.equal((await stored('yavchn-notes'))['hn-1'].text, 'Remember the benchmark table.');
    await page.locator('#row-hn-1 .note-mark[aria-label="Has a note"]').waitFor();
    assert.equal(await reader.locator('[data-note-toggle]').textContent(), 'Note');

    // The toggle is PUDL's disclosure button, whose chevron is 16 pixels,
    // and the panel stays as the reader leaves it across a reload.
    const toggle = reader.locator('[data-note-toggle]');
    assert.equal(await toggle.getAttribute('aria-expanded'), 'true');
    assert.deepEqual(await toggle.evaluate(b => { const s = getComputedStyle(b, '::after'); return [s.width, s.height, s.maskImage.includes('svg') || s.webkitMaskImage.includes('svg')]; }), ['16px', '16px', true]);
    await toggle.click();
    assert.equal(await toggle.getAttribute('aria-expanded'), 'false');
    assert.equal(await reader.locator('[data-note-panel]').isHidden(), true);
    await page.waitForTimeout(600);
    await page.reload();
    await page.locator('.win.active .story [data-note-toggle]:not([hidden])').waitFor();
    assert.equal(await reader.locator('[data-note-toggle]').getAttribute('aria-expanded'), 'false');
    assert.equal(await reader.locator('[data-note-panel]').isHidden(), true);
    await reader.locator('[data-note-toggle]').click();
    await page.waitForTimeout(600);
    await page.reload();
    await page.locator('.win.active .story [data-note-toggle][aria-expanded="true"]').waitFor();
    assert.equal(await reader.locator('[data-note-panel]').isVisible(), true);

    // Saved holds the three lists as tabs, and the windows stay open.
    await page.locator('.source-seg a[data-source="pinned"]').click();
    await page.waitForURL(u => u.pathname === '/pinned/');
    assert.equal((await page.locator('.source-seg a[data-source="pinned"]').textContent()).trim(), 'Saved');
    await page.locator('.list-tabs').getByRole('link', { name: 'Collections', exact: true }).click();
    await page.waitForURL(u => u.pathname === '/collections/');
    await page.locator('#row-hn-1 .collection-names').filter({ hasText: 'In Reading list' }).waitFor();
    assert.equal(await page.locator('.win.story-win').count(), 1);
    // Collections is a menu of checkboxes; ticking one filters at once and the menu stays open.
    await page.getByRole('button', { name: 'All collections', exact: true }).click();
    await page.locator('#collection-filter-menu').getByLabel('Reading list').check();
    await page.waitForURL(u => u.pathname === '/collections/' && u.searchParams.getAll('c').join() === id);
    await page.waitForFunction(() => document.getElementById('collection-filter-menu')?.matches(':popover-open'));
    assert.equal(await page.locator('#collection-filter-menu').getByLabel('Reading list').isChecked(), true);
    assert.ok(new URL(page.url()).searchParams.get('open'), 'the windows left the address');
    await page.locator('#row-hn-1 .story-uncollect').waitFor();
    await page.locator('[data-collection-chip] .filter-chip-label').filter({ hasText: 'Reading list' }).waitFor();
    await page.keyboard.press('Escape');
    // The Show menu's site boxes narrow the list the same way.
    await page.getByRole('button', { name: 'Show', exact: true }).click();
    await page.locator('#show-filter-menu').getByLabel('Lobsters').check();
    await page.locator('.story-list .empty-state-title').filter({ hasText: 'Nothing matches' }).waitFor();
    await page.locator('#show-filter-menu').getByLabel('Lobsters').uncheck();
    await page.locator('#row-hn-1').waitFor();
    await page.keyboard.press('Escape');
    assert.equal(await page.title(), 'Reading list · YAVCHN');
    if (process.env.YAVCHN_SHOTS) await page.screenshot({ path: process.env.YAVCHN_SHOTS + '/saved-wide.png' });

    // Rename through the collection menu, whose button then names the collection.
    await page.locator('[data-collection-menu-label]').click();
    await page.getByRole('button', { name: 'Rename this collection…', exact: true }).click();
    await dialog.getByLabel('Name').fill('Later');
    await dialog.getByRole('button', { name: 'Rename', exact: true }).click();
    await page.locator('[data-collection-chip] .filter-chip-label').filter({ hasText: 'Later' }).waitFor();
    assert.equal(await page.locator('[data-collection-menu-label]').textContent(), 'Later');

    // Notes lists the note and its words filter searches the note's text.
    await page.locator('.list-tabs').getByRole('link', { name: 'Notes', exact: true }).click();
    await page.waitForURL(u => u.pathname === '/notes/');
    await page.locator('#row-hn-1 .note-excerpt').filter({ hasText: 'Remember the benchmark table.' }).waitFor();
    await page.locator('.pin-filter input[name="q"]').fill('benchmark');
    await page.waitForTimeout(250);
    assert.equal(await page.locator('.story-list .story-row').count(), 1);
    await page.locator('.pin-filter input[name="q"]').fill('nothing-like-this');
    await page.locator('.story-list .empty-state-title').filter({ hasText: 'Nothing matches' }).waitFor();
    await page.locator('.pin-filter input[name="q"]').fill('');

    // Clearing the text deletes the note.
    await reader.getByLabel('Your note on this story').fill('');
    await reader.locator('[data-note-status]').filter({ hasText: 'Note deleted.' }).waitFor();
    assert.deepEqual(await stored('yavchn-notes'), {});
    await page.locator('.story-list .empty-state-title').filter({ hasText: 'No notes yet' }).waitFor();

    // Removing the last story, then deleting the collection.
    // The first form of a collection's address still reaches it.
    await page.goto(origin + '/collections/' + id + '/?view=window');
    await page.waitForURL(u => u.pathname === '/collections/' && u.searchParams.get('c') === id);
    await page.locator('#row-hn-1 .story-uncollect').click();
    await page.locator('.story-list .empty-state-title').filter({ hasText: 'This collection is empty' }).waitFor();
    await page.locator('[data-collection-menu-label]').click();
    await page.getByRole('button', { name: 'Delete this collection…', exact: true }).click();
    await page.locator('#collection-delete-dialog').getByRole('button', { name: /Delete the collection/ }).click();
    await page.waitForURL(u => u.pathname === '/collections/' && !u.searchParams.has('c'));
    assert.deepEqual(await stored('yavchn-collections'), {});
    await page.locator('.story-list .empty-state-title').filter({ hasText: 'No collections yet' }).waitFor();

    // An unknown collection explains itself, and a malformed one is not a page.
    await page.goto(origin + '/collections/zzzzzzzzzzzz/');
    await page.locator('.story-list .empty-state-title').filter({ hasText: 'This collection is not here' }).waitFor();
    assert.equal((await page.goto(origin + '/collections/Not-An-ID/')).status(), 404);

    // The Saved list bar fits a phone without scrolling the page sideways.
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto(origin + '/collections/?view=window');
    await page.locator('.list-tabs + .seg-menu > button').waitFor();
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
    if (process.env.YAVCHN_SHOTS) await page.screenshot({ path: process.env.YAVCHN_SHOTS + '/saved-narrow.png' });

    assert.deepEqual(errors, []);
    console.log('Collections, notes, Saved tabs, rename, delete, filters and narrow layout passed.');
  } finally {
    await browser.close();
  }
})().catch(err => { console.error(err); process.exit(1); });
