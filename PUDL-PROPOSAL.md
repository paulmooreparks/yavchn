# Proposal: what PUDL should take over from YAVCHN

This proposal is for the PUDL agent. YAVCHN wrote it on 4 October 2026, against PUDL 0.44.1. Every gap below was checked against the v0.44.1 tag that day, so please check current PUDL before implementing, in case a later release covers some of them. B3 and B7 were revised on 5 October 2026, against PUDL 0.49.0.

The proposal has two parts. Part A covers four runtime features that YAVCHN has patched into its bundled copies of `pudl-regions.js` and `pudl-windows.js`. Those patches are why YAVCHN cannot simply copy a release, and why its 0.44.1 upgrade had to take files one at a time. The adoption guide asks sites to use tagged releases rather than patching PUDL, so these are the most pressing. Part B covers patterns that YAVCHN and parkscomputing.com now implement separately, in their own stylesheets and scripts, which belong in PUDL so both can drop their copies.

Each item names where YAVCHN's version lives, so you can read the behavior that works today before designing the public contract.

## Part A. Runtime features YAVCHN patches into PUDL

### A1. Window-scoped address parameters

YAVCHN's reader windows keep the article each one shows in the address, as `r.<window-key>`, beside PUDL's own `open`, `top`, `min`, and `p.<key>`. A reader window is an instance such as `reader-1`, and its article changes as the reader reads, so the article cannot be the window key.

Two patches make this work. In `pudl-regions.js`, `isWinParam()` also treats `r.` parameters as window state, so region swaps carry them with the windows. In `pudl-windows.js`, the parameter filter drops an `r.<key>` whose window is not open, so closing a reader removes its article from the address.

Please give hosts a documented way to declare window-scoped parameter prefixes, perhaps as an attribute on the window layer or an option to `pudlWindows`. PUDL would then treat `<prefix>.<key>` as belonging to window `<key>`. It would carry such parameters through region swaps and Back and Forward, include them in a shared window link, and drop them when the window closes. YAVCHN would declare `r`.

### A2. Reloading the current regions

YAVCHN's Refresh the feed command refetches the current list without a page load and without a new history entry. Its patch adds `pudlRegions.reload()`. The function refetches `location.href` with `Cache-Control: no-cache`, swaps the regions in place, and resolves to `true`. If the fetched page has no matching regions, or the request fails, it rejects and leaves the current regions intact; it never navigates. A navigation that starts during the reload cancels it.

Please add `reload()` to `pudlRegions` with that contract, or an equivalent one, and document how it differs from following a link.

### A3. Collapsed segmented controls as region links

On a phone, PUDL folds a `data-seg-menu` segmented control into a menu button whose panel repeats each choice as a link. The region script ignores every link inside `.menu-panel`, so choosing a list tab from the collapsed control loads the whole page, and the open windows reload with it. The same choice from the full control swaps only the list.

YAVCHN's patch exempts `.seg-choice` links from the menu-panel rule, because they stand in for links that are already region links. Please make the generated choices behave as the segments they mirror. That could mean copying the original link's region behavior onto each choice, or the exemption YAVCHN uses.

### A4. Re-keying a window

A reader window that loads a different story keeps its element, placement, focus, and running content, but its key in the address changes. `pudlWindows.replace()` closes the old window and opens a new one, which loses all of that.

YAVCHN's patch adds `pudlWindows.rekey(oldKey, key, push)`. It renames the open window in the state, the minimized and placement maps, child windows' `data-win-parent`, focus memory and openers. It commits the change as a new history entry unless `push` is `false`, and dispatches `pudl:window-rekey` with both keys. Re-keying to the same key raises the window, and re-keying onto a key that is open or pending is refused.

Please adopt `rekey()` and its event as public API, with that contract, and document it beside `replace()` so hosts know which to use.

## Part B. Patterns both sites implement separately

### B1. Multi-select filter menus

parkscomputing.com's Categories and Tags menus, and YAVCHN's Show and collection menus, are menu panels of checkbox rows that filter a list. Both sites styled the rows themselves, as `.menu-panel .menu-check`, and both wrote the same behavior. Ticking a box submits the filter form, the region swap rebuilds the bar, and the menu that was open has to open again with focus back on the same box. Otherwise every tick closes the menu. parkscomputing.com does this in `desktop.js` (`syncFilterMenus`), and YAVCHN does it in `library.js`, which records the panel and box and calls `showPopover()` after `pudl:regions-swap`.

Please provide the checkbox row style, and a documented filter-menu behavior. A panel marked, say, `data-filter-menu` would submit its form on change and reopen across the swap that change causes, keeping focus on the box. In YAVCHN, each box belongs to the filter form through its `form` attribute, so the form works without script. The reference could recommend that.

### B2. A settings panel

Both sites now lay out Settings, and YAVCHN its Account window, as a column of PUDL cards. Each card has a `.card-title`, a `.card-desc` in muted type, its controls, and an action row of buttons. YAVCHN carries this as `.settings-panel`, `.settings-card`, and `.settings-actions` in `src/static/style.css`, after parkscomputing.com's `css/settings.css`. The two differ only in names.

Please add the pattern to PUDL with documented classes: the column, the card in it, and the action row. Add a reference example. YAVCHN would rename its classes to match and delete its copy.

### B3. The account placeholder

PUDL 0.48.0's status area covers most of this, and since 0.49.0 YAVCHN's account is a status item in it: the reader's picture, or the placeholder with Sign in beside it. One part is left. The head-and-shoulders placeholder is still YAVCHN's own SVG, the `avatar` template in `src/templates/partials.html.tmpl`, copied from parkscomputing.com. Please provide it as a glyph, so a status item for an account without a picture looks the same on every site.

### B7. The status area should match the menu bar's groups

The reference says the status area is one raised group whose items press in as a menu bar's titles do, and it stands beside the menu bar's groups on the same topbar, but in 0.49.0 it is drawn differently from them. Measured on YAVCHN in Edge, light and dark themes:

- The groups' surface is `--recess-bg` with `--raise-border` and `--raise-shadow`. The status area's is the topbar chip, `--tb-chip` with `--tb-border` and `--tb-chip-shadow`, a gradient that in the dark theme is clearly lighter than the groups beside it.
- A group is 30 pixels tall, a 26-pixel title inside 1 pixel of padding and a 1-pixel border. The status area is 32, because its items are 28: a 26-pixel minimum height plus 2 pixels of padding above and below.
- Titles are in `--text`, but status items are in `--tb-chrome-fg`, so Sign in reads greyer than the menu titles.
- A title presses in with `--raise-active-bg` and `--raise-active-shadow`; a status item uses its own mix.

Please draw `.status-area` and `.status-item` with the menu bar group's surface, height and title colours, or give both one shared set of tokens. YAVCHN carries a stopgap at the end of the account styles in `src/static/style.css` that does this from tokens a site may read, and will delete it when a release fixes the area.

### B4. A disclosure button

YAVCHN's Note button opens and closes the note under a story's toolbar. Following PUDL's semantics, it carries `aria-expanded` and a 16-pixel SVG chevron that turns over as it opens (`.disclosure-chevron` in `style.css`). PUDL has a caret glyph for menu buttons, but nothing for a button that discloses content in place, and Paul has asked for chevrons of a readable size.

Please add a disclosure button style: a raised button whose chevron reflects `aria-expanded`, at a size the reference sets. It should honor reduced motion.

### B5. Toasts from content loaded into a window

PUDL re-raises server-rendered toasts that are in the page when it loads. A window body fetched later, as `/window/{key}` is, has no way to carry a toast. YAVCHN's Account window needs one after an action redirects back to it, for example "Your picture was saved". The panel renders `<p data-account-toast="positive" hidden>`, and the applet script calls `pudlToast()` with its text.

Please let content mounted into a window carry `.toast` elements that PUDL moves into the toast region and announces, as it does for those present at load. The same markup would then work for a Classic page and a window.

### B6. A default identity menu

PUDL's conventions give every window an identity menu: Open as a page, Copy link, and Close window. A window whose content is not an applet with `menus()` gets no front menu at all. YAVCHN therefore registers three stub applets, for Settings, the user lookup, and profiles, only to supply that menu (`src/static/applets/identity.js`).

Please generate the identity menu, titled from the window, for any window whose content supplies no menus, so hosts need no stubs. An applet that supplies menus would keep full control, as now.

## Verification

YAVCHN's browser suites already cover the behavior above and can serve as acceptance tests once YAVCHN adopts the release:

- `tests/readers.browser.cjs` covers reader parameters, re-keying, feed reload, and collapsed segment links.
- `tests/library.browser.cjs` covers the filter menus that stay open, and the settings layout at phone width.
- `tests/signin.browser.cjs` and `tests/accounts.browser.cjs` cover the account's status item, signed in and signed out, and `tests/accounts.browser.cjs` checks that it fits a phone's width.
- `TestAccountFeedbackSitsBesideItsControl` in `src/signin_test.go` covers the toast markup; no browser test yet checks that the toast appears.

When a release covers Part A, YAVCHN will replace its patched files with the release's and delete the patches. It will adopt Part B items as they ship.
