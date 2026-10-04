const assert = require('node:assert/strict');

module.exports = async function checkReaderViewport(browser, baseURL) {
  const context = await browser.newContext({
    viewport: { width: 393, height: 727 }, deviceScaleFactor: 2.75,
    isMobile: true, hasTouch: true
  });
  try {
    const page = await context.newPage();
    page.setDefaultTimeout(8000);
    await page.route('**/api/article?*', route => route.fulfill({ contentType: 'text/html', body: '<article style="height:3000px">Article fixture</article>' }));
    await page.route('**/api/discussion?*', route => route.fulfill({ contentType: 'text/html', body: '<div>Discussion fixture</div>' }));
    const taskbarFits = async () => {
      await page.waitForFunction(() => {
        const bar = document.querySelector('.win-bar');
        if (!bar) return false;
        const rect = bar.getBoundingClientRect();
        return rect.height > 0 && rect.top >= 0 && rect.bottom <= window.visualViewport.height + 1;
      });
    };
    const switchView = async (label, value) => {
      // On a phone the menu bar is one button whose panel opens a level at a time.
      await page.locator('.menubar-one > button').tap();
      await page.getByRole('menuitem', { name: 'View', exact: true }).tap();
      await page.getByRole('menuitemcheckbox', { name: label, exact: true }).tap();
      await page.waitForFunction(view => document.body?.dataset.view === view, value);
      await page.locator('.story[data-story-key="hn-1"]').waitFor();
    };
    await page.goto(baseURL + '/hn/?view=window');
    await page.locator('#row-hn-1 > a.md-item').tap();
    await page.locator('.story[data-story-key="hn-1"]').waitFor();
    await taskbarFits();
    await switchView('Classic', 'classic');
    await switchView('Windowed', 'window');
    await taskbarFits();
    await page.setViewportSize({ width: 727, height: 393 });
    await taskbarFits();
    await page.setViewportSize({ width: 393, height: 727 });
    await taskbarFits();

    // Model a visible viewport smaller than the CSS viewport. This is a
    // contract test, not a reproduction of Android's installed-app behavior.
    await page.evaluate(() => {
      window.nativeViewport = window.visualViewport;
      window.testViewport = Object.assign(new EventTarget(), { height: 600, scale: 1 });
      Object.defineProperty(window, 'visualViewport', {
        configurable: true, value: window.testViewport
      });
      // The production listener was registered against the native object.
      window.testViewport.addEventListener('resize', () => window.nativeViewport.dispatchEvent(new Event('resize')));
      window.dispatchEvent(new PageTransitionEvent('pageshow'));
    });
    await taskbarFits();
    assert.deepEqual(await page.evaluate(() => ({
      height: document.body.getBoundingClientRect().height,
      measured: document.documentElement.style.getPropertyValue('--yv-viewport-height'),
      viewport: window.visualViewport.height,
      scale: window.visualViewport.scale
    })), { height: 600, measured: '600px', viewport: 600, scale: 1 });
    await page.evaluate(() => {
      window.testViewport.height = 540;
      window.visualViewport.dispatchEvent(new Event('resize'));
    });
    await taskbarFits();
    assert.equal(await page.evaluate(() => document.body.getBoundingClientRect().height), 540);
    // Pinch zoom must magnify the existing layout, rather than shrink it.
    await page.evaluate(() => {
      window.testViewport.height = 270;
      window.testViewport.scale = 2;
      window.visualViewport.dispatchEvent(new Event('resize'));
    });
    assert.equal(await page.evaluate(() => document.body.getBoundingClientRect().height), 540);
    // A document that is not fully active can report zero height.
    await page.evaluate(() => {
      window.testViewport.height = 0;
      window.testViewport.scale = 1;
      window.visualViewport.dispatchEvent(new Event('resize'));
    });
    assert.equal(await page.evaluate(() => document.body.getBoundingClientRect().height), 540);
    await page.evaluate(() => {
      Object.defineProperty(window, 'visualViewport', { configurable: true, value: window.nativeViewport });
      window.dispatchEvent(new Event('resize'));
    });
    await taskbarFits();
    assert.equal(await page.evaluate(() => document.body.getBoundingClientRect().height), 727);
  } finally {
    await context.close();
  }
};
