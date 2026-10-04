const assert = require('node:assert/strict');
const { chromium } = require(process.env.YAVCHN_PLAYWRIGHT || 'playwright');

// Passkeys through Chromium's virtual authenticator, and a picture cropped
// and re-encoded in the browser before upload.
(async () => {
  const [origin, session] = process.argv.slice(2);
  const browser = await chromium.launch({ channel: process.env.YAVCHN_BROWSER_CHANNEL || 'msedge', headless: true });
  try {
    const context = await browser.newContext({ ignoreHTTPSErrors: true });
    await context.addCookies([{ name: '__Host-yavchn-session', value: session, url: origin, httpOnly: true, secure: true, sameSite: 'Lax' }]);
    const page = await context.newPage();
    page.setDefaultTimeout(10000);
    const errors = [];
    page.on('pageerror', e => errors.push(e.message));
    const cdp = await context.newCDPSession(page);
    await cdp.send('WebAuthn.enable');
    await cdp.send('WebAuthn.addVirtualAuthenticator', { options: {
      protocol: 'ctap2', transport: 'internal', hasResidentKey: true, hasUserVerification: true, isUserVerified: true, automaticPresenceSimulation: true
    } });

    // Add a passkey to the signed-in account.
    await page.goto(origin + '/account?view=classic');
    await page.getByLabel('Name for a new passkey').fill('Test laptop');
    await page.getByRole('button', { name: 'Add a passkey', exact: true }).click();
    await page.waitForURL(u => u.searchParams.get('message') === 'passkey-added');
    await page.locator('.account-methods th', { hasText: 'Test laptop' }).waitFor();

    // Sign out, then back in with the passkey alone.
    await page.getByRole('button', { name: 'Sign out this browser', exact: true }).click();
    await page.waitForURL(u => u.searchParams.get('message') === 'signed-out');
    assert.equal(await page.locator('meta[name="yavchn-account"]').count(), 0);
    await page.getByRole('button', { name: 'Sign in with a passkey', exact: true }).click();
    await page.waitForURL(u => u.pathname === '/account' && !u.searchParams.has('message'));
    await page.locator('meta[name="yavchn-account"]').waitFor({ state: 'attached' });
    await page.locator('.account-methods td', { hasText: 'last used' }).waitFor();

    // Upload a 600 by 400 picture; the browser sends a 256-pixel square.
    const png = await page.evaluate(async () => {
      const canvas = document.createElement('canvas');
      canvas.width = 600; canvas.height = 400;
      const ctx = canvas.getContext('2d');
      ctx.fillStyle = '#c33'; ctx.fillRect(0, 0, 600, 400);
      ctx.fillStyle = '#33c'; ctx.fillRect(200, 100, 200, 200);
      const blob = await new Promise(r => canvas.toBlob(r, 'image/png'));
      return Array.from(new Uint8Array(await blob.arrayBuffer()));
    });
    await page.getByLabel('Choose a picture').setInputFiles({ name: 'photo.png', mimeType: 'image/png', buffer: Buffer.from(png) });
    await page.getByRole('button', { name: 'Save picture', exact: true }).click();
    await page.waitForURL(u => u.searchParams.get('message') === 'picture-saved');
    const src = await page.locator('.topbar .account-pill img.account-avatar').getAttribute('src');
    const sent = await page.evaluate(async src => {
      const r = await fetch(src);
      const bitmap = await createImageBitmap(await r.blob());
      return { type: r.headers.get('content-type'), width: bitmap.width, height: bitmap.height };
    }, src);
    assert.deepEqual(sent, { type: 'image/webp', width: 256, height: 256 });

    // Removing the passkey leaves the account's other way in.
    await page.getByRole('button', { name: 'Remove the passkey Test laptop', exact: true }).click();
    await page.waitForURL(u => u.searchParams.get('message') === 'passkey-removed');
    assert.equal(await page.locator('.account-methods th', { hasText: 'Test laptop' }).count(), 0);
    assert.deepEqual(errors, []);
    console.log('Passkey registration, passkey sign-in, cropped picture upload and passkey removal passed.');
  } finally {
    await browser.close();
  }
})().catch(err => { console.error(err); process.exit(1); });
