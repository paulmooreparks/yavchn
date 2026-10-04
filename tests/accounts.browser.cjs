const assert = require('node:assert/strict');
const { chromium } = require(process.env.YAVCHN_PLAYWRIGHT || 'playwright');

(async () => {
  const origin = process.argv[2];
  const browser = await chromium.launch({ channel: 'msedge', headless: true });
  try {
    const errors = [];
    const context = await browser.newContext({ ignoreHTTPSErrors: true });
    const page = await context.newPage();
    page.setDefaultTimeout(10000);
    page.on('pageerror', e => errors.push(e.message));
    const pin = { source: 'hn', title: 'Anonymous story', url: 'https://example.com/', host: 'example.com', by: 'reader', score: 1, comments: 0, pinned_at: 1 };
    await page.goto(origin + '/hn/');
    await page.evaluate(value => localStorage.setItem('yavchn-pinned', JSON.stringify({ 'hn-99': value })), pin);
    const session = token => ({ name: '__Host-yavchn-session', value: token, url: origin, httpOnly: true, secure: true, sameSite: 'Lax' });
    await context.addCookies([session(process.argv[3])]);
    await page.goto(origin + '/account');
    const accountID = await page.locator('meta[name="yavchn-account"]').getAttribute('content');
    assert.deepEqual(await page.evaluate(() => JSON.parse(window.yavchnStorage.getItem('yavchn-pinned'))), {});
    const uploaded = page.waitForResponse(r => r.url() === origin + '/account/data' && r.request().method() === 'PUT' && r.status() === 200);
    await page.getByRole('button', { name: "Import this browser's anonymous data" }).click();
    await uploaded;
    const imported = await page.evaluate(async () => (await (await fetch('/account/data')).json()));
    assert.ok(imported.revision > 0 && imported.data.pins['hn-99'], JSON.stringify(imported));

    await page.goto(origin + '/hn/?view=window&open=reader-1,settings&top=reader-1');
    await page.locator('[data-win="reader-1"]').waitFor();
    await page.evaluate(() => { window.accountTestReader = document.querySelector('[data-win="reader-1"]'); });
    await page.locator('.menubar-title').filter({ hasText: /^\s*YAVCHN\s*$/ }).click();
    await page.getByRole('menuitem', { name: /^Account \(/ }).click();
    await page.locator('[data-win="account"] [data-account-import]:not([hidden])').waitFor();
    assert.equal(await page.locator('body').getAttribute('data-view'), 'window');
    assert.equal(await page.evaluate(() => window.accountTestReader === document.querySelector('[data-win="reader-1"]')), true);
    await page.locator('.menubar-front .menubar-title').filter({ hasText: /^Account$/ }).click();
    await page.getByRole('menuitem', { name: 'Copy account link', exact: true }).waitFor();
    await page.keyboard.press('Escape');
    await page.locator('[data-win="account"] [data-win-action="page"]').click();
    await page.locator('body[data-view="classic"] [data-applet="account"]').waitFor();
    assert.equal(await page.locator('.win').count(), 0);
    await page.getByRole('button', { name: 'Windowed', exact: true }).click();
    await page.locator('body[data-view="window"] [data-win="account"] [data-applet="account"]').waitFor();
    assert.ok(new URL(page.url()).searchParams.get('open').includes('account'));

    const secondDevice = await browser.newContext({ ignoreHTTPSErrors: true });
    await secondDevice.addCookies([session(process.argv[3])]);
    const secondPage = await secondDevice.newPage();
    secondPage.on('pageerror', e => errors.push(e.message));
    await secondPage.goto(origin + '/account');
    const secondState = await secondPage.evaluate(() => ({ pins: JSON.parse(window.yavchnStorage.getItem('yavchn-pinned')), boot: document.getElementById('yavchn-account-data').textContent }));
    assert.equal(secondState.pins['hn-99']?.title, pin.title, JSON.stringify(secondState));
    await page.goto(origin + '/hn/');
    const pinned = page.waitForResponse(r => r.url() === origin + '/account/data' && r.request().method() === 'PUT' && r.status() === 200);
    await page.locator('#row-hn-1 .story-pin').click();
    await pinned;
    // This tab has an older revision. Its unrelated change must retain the newer pin.
    const conflict = secondPage.waitForResponse(r => r.url() === origin + '/account/data' && r.request().method() === 'PUT' && r.status() === 412);
    const saved = secondPage.waitForResponse(r => r.url() === origin + '/account/data' && r.request().method() === 'PUT' && r.status() === 200);
    await secondPage.evaluate(() => window.yavchnStorage.setItem('yavchn-blocked-domains', JSON.stringify(['example.org'])));
    await conflict;
    await saved;
    const merged = await secondPage.evaluate(async () => (await (await fetch('/account/data')).json()).data);
    assert.ok(merged.domains.includes('example.org') && merged.pins['1'] && merged.pins['hn-99'], JSON.stringify(merged));
    const removed = secondPage.waitForResponse(r => r.url() === origin + '/account/data' && r.request().method() === 'PUT' && r.status() === 200);
    await secondPage.evaluate(() => {
      const pins = JSON.parse(window.yavchnStorage.getItem('yavchn-pinned'));
      delete pins['hn-99'];
      window.yavchnStorage.setItem('yavchn-pinned', JSON.stringify(pins));
    });
    await removed;
    await page.goto(origin + '/account');
    assert.equal(await page.evaluate(() => JSON.parse(window.yavchnStorage.getItem('yavchn-pinned'))['hn-99']), undefined);

    await context.addCookies([session(process.argv[4])]);
    await page.goto(origin + '/account');
    assert.notEqual(await page.locator('meta[name="yavchn-account"]').getAttribute('content'), accountID);
    assert.deepEqual(await page.evaluate(() => JSON.parse(window.yavchnStorage.getItem('yavchn-pinned'))), {});
    const loggedOut = page.waitForResponse(r => r.url() === origin + '/account/session' && r.request().method() === 'POST');
    await page.getByRole('button', { name: 'Sign out this browser', exact: true }).click();
    const logoutResponse = await loggedOut;
    if (logoutResponse.status() !== 303) assert.fail(await logoutResponse.text());
    await page.waitForURL(url => url.pathname === '/account' && url.searchParams.get('message') === 'signed-out');
    assert.equal(await page.locator('meta[name="yavchn-account"]').count(), 0);
    assert.equal(await page.evaluate(() => JSON.parse(window.yavchnStorage.getItem('yavchn-pinned'))['hn-99'].title), pin.title);
    // A quota failure must not return an older stored value after a successful edit.
    assert.equal(await page.evaluate(() => {
      const original = Storage.prototype.setItem;
      Storage.prototype.setItem = function () { throw new DOMException('Full', 'QuotaExceededError'); };
      try {
        window.yavchnStorage.setItem('yavchn-blocked-domains', '["quota.example"]');
        return JSON.parse(window.yavchnStorage.getItem('yavchn-blocked-domains'))[0];
      } finally { Storage.prototype.setItem = original; }
    }), 'quota.example');
    await secondPage.goto(origin + '/hn/?view=window&open=reader-1,account&top=account');
    await secondPage.locator('[data-win="account"] [data-account-import]:not([hidden])').waitFor();
    await secondPage.getByRole('button', { name: 'Sign out this browser', exact: true }).click();
    await secondPage.waitForURL(url => url.pathname === '/hn/' && url.searchParams.get('message') === 'signed-out');
    await secondPage.locator('[data-win="account"] [data-applet="account"]').waitFor();
    assert.equal(await secondPage.locator('body').getAttribute('data-view'), 'window');
    assert.ok(await secondPage.locator('[data-win="account"]').textContent().then(s => s.includes('You are signed out.')));
    assert.equal(await secondPage.locator('[data-win="reader-1"]').count(), 1);
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto(origin + '/account?view=window');
    await page.locator('[data-win="account"] [data-applet="account"]').waitFor();
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
    assert.deepEqual(errors, []);
    await secondDevice.close();
    await context.close();
    console.log('Account browser checks passed: explicit import, multi-device conflicts, deletion, account isolation, logout, mobile layout.');
  } finally { await browser.close(); }
})().catch(error => { console.error(error); process.exitCode = 1; });
