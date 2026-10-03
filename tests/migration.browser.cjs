const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const { chromium } = require(process.env.YAVCHN_PLAYWRIGHT || 'playwright');

(async () => {
  const browser = await chromium.launch({ channel: process.env.YAVCHN_BROWSER_CHANNEL || 'msedge', headless: true });
  try {
    const context = await browser.newContext({ viewport: { width: 390, height: 844 }, acceptDownloads: true });
    const errors = [];
    await context.route(/https:\/\/(yavchn\.parkscomputing\.com|(?:www\.)?yavchn\.com)\//, async route => {
      const url = new URL(route.request().url());
      const response = await context.request.get(process.argv[2] + url.pathname + url.search);
      await route.fulfill({ response });
    });
    const page = await context.newPage();
    page.on('pageerror', error => errors.push(error.message));
    await page.goto('https://yavchn.parkscomputing.com/hn/');
    await page.getByRole('link', { name: 'Start migration' }).waitFor();
    assert.equal(await page.locator('.toast[data-toast-sticky]').count(), 1);
    await page.evaluate(() => {
      localStorage.setItem('yavchn-roundtrip', 'A Unicode value: 日本語');
      localStorage.setItem('yavchn-visited', '["hn-1"]');
      localStorage.setItem('pudl-theme', 'dark');
    });
    await page.getByRole('link', { name: 'Start migration' }).click();
    await page.waitForFunction(() => document.querySelector('[data-migration]')?.dataset.migrationReady === 'true');
    assert.equal(await page.locator('[data-migration-import]').isVisible(), false);
    const downloaded = page.waitForEvent('download');
    await page.getByRole('button', { name: 'Save browser data to a file' }).click();
    const download = await downloaded;
    const text = await fs.readFile(await download.path(), 'utf8');
    const backup = JSON.parse(text);
    assert.equal(backup.origin, 'https://yavchn.parkscomputing.com');
    assert.equal(backup.storage['yavchn-roundtrip'], 'A Unicode value: 日本語');
    assert.equal(await page.evaluate(() => localStorage.getItem('yavchn-roundtrip')), backup.storage['yavchn-roundtrip']);
    await page.getByRole('link', { name: 'Continue to yavchn.com' }).click();
    assert.equal(new URL(page.url()).hostname, 'yavchn.com');
    assert.equal(await page.getByRole('link', { name: 'Start migration' }).count(), 0);
    await page.evaluate(() => {
      localStorage.setItem('yavchn-roundtrip', 'previous');
      localStorage.setItem('destination-only', 'preserved');
    });
    const choose = content => page.locator('[data-migration-file]').setInputFiles({ name: 'backup.json', mimeType: 'application/json', buffer: Buffer.from(content) });
    const apply = page.getByRole('button', { name: 'Import browser data' });
    await choose('{');
    await page.getByText('This file is not valid JSON. Choose a YAVCHN backup.').waitFor();
    assert.equal(await apply.isDisabled(), true);
    await choose(JSON.stringify({ ...backup, storage: { bad: 1 } }));
    await page.getByText('Choose a valid YAVCHN browser data backup.').waitFor();
    assert.equal(await page.evaluate(() => localStorage.getItem('yavchn-roundtrip')), 'previous');
    await choose(text);
    await apply.waitFor({ state: 'visible' });
    await page.waitForFunction(() => !document.querySelector('[data-migration-apply]').disabled);
    // A quota failure after the first write must restore matching and new entries.
    await page.evaluate(() => {
      const original = Storage.prototype.setItem;
      let count = 0;
      Storage.prototype.setItem = function (key, value) {
        if (++count === 2) throw new DOMException('Quota exceeded', 'QuotaExceededError');
        return original.call(this, key, value);
      };
    });
    await apply.click();
    await page.getByText('Import failed. Your previous data was restored. Check available browser storage and try again.').waitFor();
    assert.equal(await page.evaluate(() => localStorage.getItem('yavchn-roundtrip')), 'previous');
    assert.equal(await page.evaluate(() => localStorage.getItem('destination-only')), 'preserved');
    await apply.click();
    await page.waitForURL('https://yavchn.com/hn/');
    await page.getByText('Browser data imported.', { exact: true }).waitFor();
    assert.equal(await page.evaluate(() => localStorage.getItem('yavchn-roundtrip')), backup.storage['yavchn-roundtrip']);
    assert.equal(await page.evaluate(() => localStorage.getItem('destination-only')), 'preserved');
    assert.equal(await page.locator('html').getAttribute('data-theme'), 'dark');
    await page.goto('https://www.yavchn.com/settings');
    await choose(text);
    await page.waitForFunction(() => !document.querySelector('[data-migration-apply]').disabled);
    await apply.click();
    await page.waitForURL('https://www.yavchn.com/hn/');
    assert.equal(await page.evaluate(() => localStorage.getItem('yavchn-roundtrip')), backup.storage['yavchn-roundtrip']);
    assert.deepEqual(errors, []);
    console.log('PASS: legacy toast, mobile export, both new domains, validation, rollback, and storage round-trip');
  } finally { await browser.close(); }
})().catch(error => { console.error(error); process.exit(1); });
