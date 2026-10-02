const assert = require('node:assert/strict');
const { chromium } = require(process.env.YAVCHN_PLAYWRIGHT || 'playwright');

(async function () {
  const browser = await chromium.launch({ channel: process.env.YAVCHN_BROWSER_CHANNEL || 'msedge', headless: true });
  try {
    const page = await browser.newPage({ viewport: { width: 1500, height: 1000 } });
    page.setDefaultTimeout(8000);
    const errors = [];
    page.on('pageerror', e => errors.push(e.message));
    await page.route('**/api/article?*', route => route.fulfill({ contentType: 'text/html', body: '<article style="height:3000px">Article fixture</article>' }));
    await page.route('**/api/discussion?*', route => route.fulfill({ contentType: 'text/html', body: '<div class="discussion-content" style="height:3000px">Discussion fixture</div>' }));
    await page.goto(process.argv[2] + '/hn/');
    await page.waitForFunction(() => window.yavchn.readers && window.pudlWindows);
    const story = (key, article) => page.waitForFunction(([k, a]) => document.querySelector(`.win[data-win="${k}"] .story`)?.dataset.storyKey === a, [key, article]);
    const siteBadge = async (key, source, label) => {
      await page.waitForFunction(([k, s]) => document.querySelector(`[data-win-tab="${k}"]`)?.getAttribute('data-reader-source') === s, [key, source]);
      assert.equal(await page.locator(`[data-win="${key}"] .win-head .reader-site .visually-hidden`).count(), 1);
      assert.equal((await page.locator(`[data-win="${key}"] .win-head .reader-site`).textContent()).trim(), label);
      assert.equal((await page.locator(`[data-win-tab="${key}"] .reader-site`).innerText()).trim(), label);
      await page.waitForFunction(k => [...document.querySelectorAll(`[data-win="${k}"] .reader-site img, [data-win-tab="${k}"] .reader-site img`)].every(img => img.complete && img.naturalWidth > 0), key);
    };
    await page.waitForFunction(() => [...document.querySelectorAll('.source-seg .source-icon')].filter(img => img.complete && img.naturalWidth > 0).length === 2);
    assert.equal(await page.locator('.source-seg [data-source="pinned"] .source-glyph-pin').count(), 1);
    assert.equal(await page.locator('.source-seg [data-source="find"] .source-glyph-find').count(), 1);
    const clickRow = async id => page.locator(`#row-hn-${id} > a.md-item`).click();
    const refreshFeed = async () => {
      const before = page.url();
      await page.evaluate(() => {
        window.feedRefreshCheck = {
          list: document.querySelector('.story-list'),
          reader: document.querySelector('.win.active .story'),
          history: history.length
        };
      });
      const requested = page.waitForRequest(req => req.headers()['cache-control'] === 'no-cache' && req.url() === before);
      await page.getByRole('menuitem', { name: 'Feed', exact: true }).click();
      await page.getByRole('menuitem', { name: 'Refresh feed', exact: true }).click();
      await requested;
      await page.waitForFunction(() => document.querySelector('.feed-refresh-status')?.textContent === 'Feed refreshed.');
      assert.equal(page.url(), before);
      assert.equal(await page.evaluate(() => window.feedRefreshCheck.list !== document.querySelector('.story-list') && window.feedRefreshCheck.reader === document.querySelector('.win.active .story') && window.feedRefreshCheck.history === history.length), true);
      // A failed refresh must keep the list and readers usable without navigation.
      await page.route(before, route => route.fulfill({ status: 503, contentType: 'text/html', body: 'Unavailable' }));
      await page.evaluate(() => { window.feedRefreshCheck.list = document.querySelector('.story-list'); });
      await page.locator('#feed-refresh').click();
      await page.getByText('The feed could not be refreshed. Try again.', { exact: true }).waitFor();
      assert.equal(await page.locator('#feed-refresh').isEnabled(), true);
      assert.equal(await page.evaluate(() => window.feedRefreshCheck.list === document.querySelector('.story-list') && window.feedRefreshCheck.reader === document.querySelector('.win.active .story')), true);
      await page.unroute(before);
    };
    await clickRow('1');
    await story('reader-1', 'hn-1');
    await siteBadge('reader-1', 'hn', 'HN');
    await refreshFeed();
    await page.evaluate(() => window.pudlWindows.dock('reader-1', 'left'));
    await clickRow('2');
    await story('reader-2', 'hn-2');
    await story('reader-1', 'hn-1');
    await page.locator('#row-hn-1 [data-reader-new]').click();
    await story('reader-3', 'hn-1');
    await page.evaluate(() => window.pudlWindows.minimize('reader-3'));
    await siteBadge('reader-3', 'hn', 'HN');
    await clickRow('1');
    await story('reader-2', 'hn-1');
    await story('reader-3', 'hn-1');
    await page.evaluate(() => window.pudlWindows.raise('reader-1'));
    await clickRow('2');
    await story('reader-2', 'hn-2');
    await story('reader-1', 'hn-1');
    await page.evaluate(() => window.yavchn.nextStory(document.querySelector('[data-win="reader-1"]')).click());
    await story('reader-1', 'hn-2');
    await page.goBack();
    await story('reader-1', 'hn-1');
    await page.goForward();
    await story('reader-1', 'hn-2');
    await page.reload();
    await story('reader-1', 'hn-2');
    await story('reader-2', 'hn-2');
    assert.equal(await page.locator('[data-win="reader-1"]').getAttribute('data-win-mode'), 'dock-left');
    await page.getByRole('menuitem', { name: 'Window', exact: true }).click();
    await page.getByRole('menuitem', { name: 'New reader window', exact: true }).click();
    await page.locator('[data-win="reader-4"]').waitFor();
    assert.match(await page.locator('[data-win="reader-4"]').innerText(), /Select an article/);
    assert.equal(await page.locator('[data-win="reader-4"] .reader-site, [data-win-tab="reader-4"] .reader-site').count(), 0);
    await clickRow('1');
    await story('reader-4', 'hn-1');
    await siteBadge('reader-4', 'hn', 'HN');
    await page.goBack();
    await page.waitForFunction(() => !document.querySelector('[data-win="reader-4"] .story')?.dataset.storyKey);
    assert.equal(await page.locator('[data-win="reader-4"] .reader-site, [data-win-tab="reader-4"] .reader-site').count(), 0);
    await page.goForward();
    await story('reader-4', 'hn-1');
    await page.evaluate(() => window.pudlWindows.close('reader-4'));
    assert.equal(new URL(page.url()).searchParams.has('r.reader-4'), false);
    // A list navigation carries the reader assignments through region swaps.
    await page.locator('a[href="/pinned/"]').first().evaluate(a => a.click());
    await page.waitForURL('**/pinned/**');
    assert.equal(new URL(page.url()).searchParams.get('r.reader-1'), 'hn-2');
    await page.reload();
    await story('reader-1', 'hn-2');
    // Ordinary browsing keeps one reader per source, even across list swaps.
    await page.goto(process.argv[2] + '/hn/');
    await clickRow('1');
    await story('reader-1', 'hn-1');
    await page.locator('a[data-source="lobsters"]').click();
    await page.locator('#row-lobsters-1 > a.md-item').click();
    await story('reader-2', 'lobsters-1');
    await siteBadge('reader-2', 'lobsters', 'Lobsters');
    await refreshFeed();
    if (process.env.YAVCHN_SCREENSHOT) await page.screenshot({ path: process.env.YAVCHN_SCREENSHOT });
    await story('reader-1', 'hn-1');
    await page.locator('a[data-source="hn"]').click();
    await clickRow('2');
    await story('reader-1', 'hn-2');
    await story('reader-2', 'lobsters-1');
    await page.getByRole('menuitem', { name: 'Window', exact: true }).click();
    await page.getByRole('menuitem', { name: 'New reader window', exact: true }).click();
    await page.locator('[data-win="reader-3"]').waitFor();
    await page.locator('a[data-source="lobsters"]').click();
    await page.locator('#row-lobsters-2 > a.md-item').click();
    await story('reader-3', 'lobsters-2');
    await siteBadge('reader-3', 'lobsters', 'Lobsters');
    await story('reader-2', 'lobsters-1');
    await page.locator('a[data-source="hn"]').click();
    await clickRow('1');
    await story('reader-1', 'hn-1');
    await story('reader-3', 'lobsters-2');
    // Explicit reader navigation updates its source marker and browser history.
    await page.evaluate(() => window.yavchn.readers.read('lobsters-1', null, document.querySelector('[data-win="reader-1"]')));
    await story('reader-1', 'lobsters-1');
    await siteBadge('reader-1', 'lobsters', 'Lobsters');
    await page.goBack();
    await story('reader-1', 'hn-1');
    await siteBadge('reader-1', 'hn', 'HN');
    // Two views of one article retain separate reading positions on reload.
    await page.goto(process.argv[2] + '/hn/?open=reader-1,reader-2&r.reader-1=hn-1&r.reader-2=hn-1');
    await page.waitForFunction(() => document.querySelectorAll('.discussion-content').length === 2);
    await page.evaluate(() => {
      document.querySelector('[data-win="reader-1"] .story-discussion-body').scrollTop = 150;
      document.querySelector('[data-win="reader-2"] .story-discussion-body').scrollTop = 450;
    });
    await page.waitForFunction(() => {
      const s = JSON.parse(localStorage.getItem('yavchn-applet-state'));
      return s['reader-1:hn-1']?.s.includes('d=150') && s['reader-2:hn-1']?.s.includes('d=450');
    });
    await page.reload();
    await page.waitForFunction(() => document.querySelector('[data-win="reader-1"] .story-discussion-body')?.scrollTop === 150 && document.querySelector('[data-win="reader-2"] .story-discussion-body')?.scrollTop === 450);
    await page.setViewportSize({ width: 600, height: 900 });
    await page.locator('.source-switch .menu-btn').click();
    assert.match(await page.locator('.source-switch .menu-btn-label').evaluate(el => getComputedStyle(el, '::before').backgroundImage), /hn-favicon/);
    assert.match(await page.locator('.source-switch .seg-choice[href="/lobsters/"]').evaluate(el => getComputedStyle(el, '::before').backgroundImage), /lobsters-favicon/);
    // Returning to the feed on mobile minimizes readers. Each source must
    // restore its own instance across repeated selections and source switches.
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto(process.argv[2] + '/hn/');
    await clickRow('1');
    await story('reader-1', 'hn-1');
    await page.locator('.md-back[data-win-back]').click();
    await clickRow('2');
    await story('reader-1', 'hn-2');
    assert.equal(await page.locator('.win.story-win').count(), 1);
    const mobileSource = async source => {
      await page.locator('.source-switch .menu-btn').click();
      await page.locator(`.source-switch .seg-choice[href="/${source}/"]`).click();
      await page.waitForURL(`**/${source}/**`);
    };
    await page.locator('.md-back[data-win-back]').click();
    await mobileSource('lobsters');
    await page.locator('#row-lobsters-1 > a.md-item').click();
    await story('reader-2', 'lobsters-1');
    await page.locator('.md-back[data-win-back]').click();
    await page.locator('#row-lobsters-2 > a.md-item').click();
    await story('reader-2', 'lobsters-2');
    await page.locator('.md-back[data-win-back]').click();
    await mobileSource('hn');
    await clickRow('1');
    await story('reader-1', 'hn-1');
    await page.locator('.md-back[data-win-back]').click();
    await page.reload();
    await clickRow('2');
    await story('reader-1', 'hn-2');
    assert.equal(await page.locator('.win.story-win').count(), 2);
    await story('reader-2', 'lobsters-2');
    // The same minimized-reader reuse applies after widening the viewport.
    await page.locator('.md-back[data-win-back]').click();
    await page.setViewportSize({ width: 1500, height: 1000 });
    await clickRow('1');
    await story('reader-1', 'hn-1');
    assert.equal(await page.locator('.win.story-win').count(), 2);
    assert.deepEqual(errors, []);
    const nojs = await browser.newContext({ javaScriptEnabled: false });
    const plain = await nojs.newPage();
    await plain.goto(process.argv[2] + '/hn/?open=reader-1&r.reader-1=hn-1');
    await plain.locator('[data-reader-new="hn-1"]').click();
    assert.equal(await plain.locator('.win .story[data-story-key="hn-1"]').count(), 2);
    await plain.getByRole('link', { name: 'New reader window', exact: true }).click();
    assert.equal(await plain.locator('.win').count(), 3);
    await nojs.close();
    console.log('Reader creation, per-site reuse, docking, minimization, duplicates, independent scroll positions, history, reload, list navigation and no-JS links passed.');
  } finally { await browser.close(); }
})().catch(error => { console.error(error); process.exitCode = 1; });
