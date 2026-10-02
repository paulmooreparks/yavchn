# Reader windows

Reader windows have stable instance keys such as `reader-1`. The `r.<window-key>` query parameter names the article displayed in that instance. PUDL continues to own placement, minimization and stacking. Reload and browser history restore both the arrangement and each instance's article. Existing article-key URLs remain supported.

The Applets menu starts with New reader window, which opens an empty floating reader. Applets follows Go and precedes Feed. An article row offers Open in new reader window, which creates a floating reader containing that article.

Sidebar navigation uses the most recently active undocked reader for the article's source, preferring a visible reader and otherwise restoring a minimized reader. An empty reader can take any source and acquires that source when an article is selected. Navigation creates a reader when none qualifies. Switching between HN and Lobsters therefore preserves a reader for each site by default. Docking protects an article from sidebar navigation. Minimizing a reader does not prevent reuse; the mobile Stories action minimizes readers to reveal the feed. Next story remains an explicit action on its own reader, including docked readers; it can change the reader's source when navigating a mixed list.

Each instance remembers its position within each article independently. Reader assignments are carried across list changes. Closing a reader removes its assignment from the current URL. The server renders empty readers and article assignments without JavaScript; links to create readers preserve the current list and windows.

The bundled PUDL regions script recognizes YAVCHN's `r.` parameters as persistent desktop state. Collapsed segment-menu links within regions use the same navigation as their desktop counterparts, preserving open readers when the source changes on mobile. The bundled windows script drops assignments for closed windows when writing its URL. These host integration changes must be retained when updating PUDL.

The cross-project principles remain defined in `C:\Users\paul\OneDrive\Documents\Architectural Principles.md`.

Reader title bars show the source favicon before the article title. The source name is available to screen readers without visible site text. Task-bar entries repeat the icon and label, including minimized readers. The source selector immediately to the right of the menus shows each site's favicon beside its name, a pin glyph beside Pinned, and a search glyph beside Find. The narrow-screen selector keeps these icons. These are local copies of the icons declared by the sites: HN's `https://news.ycombinator.com/y18.svg` and Lobsters' `https://lobste.rs/touch-icon-144.png`. Empty readers have no source marker. The marker follows the displayed article across navigation and browser history.

## Feed refresh

The Feed menu sits between Applets and View. Its Refresh feed command uses the same action as the looped-arrow toolbar button and is disabled when no feed is available or a refresh is running. The looped-arrow button at the start of the feed toolbar refreshes the current HN or Lobsters list, including the current list tab, page, or HN search. PUDL's host extension `pudlRegions.reload()` revalidates and replaces the current regions without replacing reader windows or adding history. Navigation cancels an outstanding refresh. Failure leaves the previous feed in place and exposes a retry message. The request uses `Cache-Control: no-cache`; source feeds refetch their IDs and refresh or invalidate their item summaries. A refresh replaces any pages appended by infinite scrolling with the page named in the URL.

## Archive lookup after reader failures

The reader preserves upstream HTTP status codes and displays them when extraction fails. Responses 401, 403, 404, 410 and 451 offer a secondary "Look for an archived copy" link alongside "Open the article". The link opens the Internet Archive Wayback calendar for the original article URL. It does not assert that a capture exists or submit the page for archiving. Other failures retain the original-page action alone. Archive URLs preserve the article query but omit credentials and fragments.

The calendar URL format is documented in the [Internet Archive Wayback CDX documentation](https://github.com/internetarchive/wayback/blob/master/wayback-cdx-server/README.md#filtering).

The article toolbar places Open original before Refresh, with Next story at the far right. Next story retains its position when a story has no external article.

## Window and Classic views

Window and Classic are explicit browsing views, independent of viewport width. Feed URLs accept `view=window` or `view=classic`. An explicit view takes priority over the browser preference. An address that names open windows implies Window when no view is specified. Other feed addresses use the saved preference, defaulting to Window. Standalone story and applet routes always render Classic pages, including shared links opened by someone whose preference is Window.

View menu choices submit to `POST /settings/view`, save the preference in the browser's `yavchn-view` cookie, and navigate to the selected view. Switching from a window to Classic keeps the active article or applet as a page. Switching a standalone page to Window opens that content in the desktop. One-off Open as a page and Open in a window links do not modify the preference. Classic feeds use the full content width, navigate to story pages, and provide a return link from each story to its source feed.

Menu visibility uses a server-rendered page context, borrowing Parks Computing's server-side `when` filtering approach without introducing its configuration format. Standalone pages omit Feed, Window, and the story-list visibility command. Classic feeds retain Feed controls. Theme and Domain filters apply across views; Applets links open standalone pages outside the desktop. Story pages omit Next story and Close. Discussion commands are disabled without comments, and article refresh is disabled while fetching. Keyboard shortcuts use the same contextual restrictions as menu commands. No window or classic mode is inferred from screen width.

The feed selector and its matching Feed menu entries appear only on stories views, including Classic feeds, Pinned, and Find. Standalone Classic article and applet pages omit both the selector and its divider. Articles retain their Back to stories link.

The top bar exposes a Windowed / Classic segmented selector immediately before the feed selector. It remains available on standalone pages and submits the same preference forms as the View menu. At narrow widths it uses the existing segmented-control dropdown. The visible mode name is Windowed; URL and cookie values remain `window`.

The view selector pairs Windowed with overlapping window outlines and Classic with a single window outline. The glyphs follow the text color and appear in both the full selector and its mobile dropdown, which retains a separate selection tick.

## Settings

YAVCHN > Settings opens the `settings` window on the desktop and navigates to `/settings` in Classic view. The same template provides default browsing view, theme, default comment order, and blocked domains. Preferences are scoped to the current browser. Settings reuses the existing theme and sort APIs, view cookie, and domain-filter storage; it does not keep a second set of preferences. The domain editor is shared with View > Domain filters, and both editors refresh together.

Default browsing view, comment order, and theme use PUDL segmented buttons with `aria-pressed` selection state. All choices remain visible at narrow widths. Changes apply immediately. Updating the default browsing view saves the cookie through the existing POST endpoint without navigating away or changing the current workspace. The view selector and View menu continue to switch the current view and save the default together. A failed default-view save restores the prior selection and displays a retry message. Without JavaScript, the default-view form still submits normally; controls requiring browser storage are disabled with an explanation. Settings does not expose automatically saved window placement or reading progress as preferences.

The Settings window uses PUDL's `data-win-size="content"` behavior. Its dimensions follow its content, its title bar remains draggable, and resizing, maximizing, snapping, and docking are unavailable. Content scrolls when it reaches the workspace bounds.

## Menu adoption in PUDL 0.40.0

The site menu order is YAVCHN, Go, Applets, Feed, View, Window, Help. Each menu declares a stable PUDL ID. Feed remains a separate site menu by user decision, overriding the adoption guide's suggestion to move its destinations into Go. It retains Hacker News, Lobsters, Pinned stories, Find discussions, and Refresh feed, with the existing stories-view visibility rules. Go contains clearly labeled external source-site links and the active reader's navigation contribution.

The identity menu provides Home, About YAVCHN, and Settings. About contains the repository link. New reader window moves from Window to Applets. The Window menu uses PUDL's generated active-window controls, bulk actions, and open-window list; the handwritten management entries are removed. Content-sized Settings receives only supported controls. Help contains site and keyboard help; external navigation moves to Go.

Reader provides Open as a page, Copy story link, and Close window where supported. Story keeps pinning, original/source links, article refresh, hiding, and blocking a domain across all feeds. Discussion keeps sorting and thread expansion. Next story and comment navigation move into Go under Reader. Sharing and closing are removed from Story. Replies separates identity commands from Watch operations. Hiring separates identity commands from its Search operations. The server-rendered profile and lookup views have no existing working menus to reorganize.

Explicit applet identity menus use the documented window command API for supported page, sharing, and close operations. Standalone pages offer sharing without window-close commands. PUDL owns contribution routing and target lifetime. A reader calls the documented menu refresh API after replacing its article because its menu inventory changes within the same applet instance. No new menu-runtime patches are introduced; the existing reader URL and feed-refresh integrations remain in the bundled regions and windows scripts.

Script and stylesheet URLs carry a hash of the embedded static asset set. Lazy applet scripts use the same version, so a deployment cannot combine cached scripts from the previous release with the new shell. The version is computed when templates are parsed and changes automatically with asset contents.

## Mobile review and PUDL handoff

The consolidated [PUDL proposal](PUDL-PROPOSAL.md) is ready for handoff. The original reports below explain the review; the accepted implementation and handoff distinguish completed site changes from pending shared-runtime work.

1. The mobile Window menu cuts off its open-window entries beneath the Open windows heading. Auto-hiding scrollbars leave no persistent indication of overflow. PUDL currently caps menu panels at 32rem. The proposed shared fix is to use the available viewport height on phones and provide persistent directional cues whenever more menu content exists above or below.
2. The article/discussion splitter is difficult to drag with a finger, and keeping both panes visible wastes scarce reading space on phones. The proposed YAVCHN control is an Article / Discussion / Split selector, with matching mutually exclusive commands in View under Reader. Single-pane modes use the reader's full content height and keep the selector accessible. Switching preserves both scroll positions, the loaded content, and the previous split ratio. The choice belongs to each reader instance and should be addressable in its URL state. The initial mobile mode remains undecided. PUDL should provide a larger touch target for splitters and, if its current public API cannot support this cleanly, a documented way to show either pane or both while retaining the split ratio. Those generic capabilities belong in the later PUDL proposal; the article/discussion labels and menu contributions belong in YAVCHN.
3. The taskbar is inside the detail area, so returning to the story list on a phone hides access to open windows. The recommendation is a full-width taskbar at the bottom of the Windowed workspace on both desktop and mobile, outside the sidebar/detail visibility switch. Both panes reserve space above it rather than letting it cover content. It remains available while browsing stories, including when readers are minimized; selecting a window restores it and switches to the detail view on phones. Classic pages do not acquire a taskbar because they have no window workspace. YAVCHN owns the layout change; the PUDL proposal should identify any missing documented support for a workspace-level taskbar independent of the detail pane.

### Proposed responsive reader layout

This layout was implemented and refined during local review. PUDL's raised buttons, recessed segmented selectors, flat informational text, and scoped menus remain the control grammar. Responsive layout changes presentation without switching the user's Windowed or Classic preference.

The reader has one identity row and one primary toolbar. On a narrow screen, the identity row combines the source favicon, the truncated story title, and access to window actions. The separate Stories strip disappears. Desktop readers retain their normal draggable title bar and window buttons; compact readers move secondary window actions into the existing window menu through supported PUDL capabilities. The full title remains available in the content and accessible name.

The primary toolbar contains an Article / Discussion / Split segmented selector and Next story at a stable right-hand position. The selector remains visible in every pane mode and never collapses into a dropdown. Next story is disabled at the end of a list and absent on standalone pages without list navigation. In single-pane mode the divider and the other pane's header disappear. Changing panes preserves loaded content, scroll positions, and split proportions. The proposed initial choice for a newly opened reader is Article on narrow screens and Split where both panes have useful room. Text-only submissions retain their post text in Article; a submission without an article or post body initially shows Discussion. An explicit or restored choice takes precedence, and resizing an existing reader does not silently change its mode. This default was accepted during review.

Open original becomes a link near the article title in the scrolling content rather than a permanent toolbar row. Article refresh remains in Story, with a direct retry action on load failure. Discussion metadata, its source-site link, and comment ordering occupy a compact header inside the discussion's scroll area. Comment ordering also remains in Discussion. Split mode uses the same content headers without repeating a separate ARTICLE or DISCUSSION command bar. The pane selector supplies those labels. No separate hide-discussion toggle is added because Article already expresses that state.

The shell keeps the menu and feed selector visible on phones. If horizontal space is insufficient, the secondary Windowed/Classic toolbar selector yields to its existing View menu commands before primary reading controls are compressed. Wider layouts retain it. The feed toolbar places refresh beside search and keeps list-category selection on a second row only when needed. The full-width taskbar remains outside both scrollable panes in Windowed mode, with a compact visible window count or overflow control when all window entries cannot fit. Safe-area insets and the on-screen keyboard must not conceal window navigation or the focused input.

Layout decisions should use the reader's available width, including narrow desktop windows, rather than device detection. Touch targets should aim for at least 44 CSS pixels without making all visual glyphs that large. A thicker visible splitter and a larger hit area must not overlap adjacent links or steal ordinary content scrolling. The intermediate layout retains optional controls only where they fit; it does not wrap window chrome into extra rows. Validation should cover 320, 390, and 768 CSS-pixel widths, narrow floating windows on a wide desktop, landscape phones, enlarged text, and keyboard-only operation.

YAVCHN owns the action priorities, reader toolbar, metadata placement, URL state, and feed/taskbar layout. The eventual PUDL proposal should cover menu overflow affordances, touch splitter geometry, supported single-pane presentation, and any missing public hooks for compact window chrome or taskbar placement. Shared runtime changes must be delivered by PUDL rather than copied into the site's vendor files.


### Control audit and accepted implementation

The responsive layout was accepted after local review on port 8080. No control in the supplied desktop screenshot is discarded. The mockup's simplified feed and window menus are not carried into the application. The following inventory records the actual destinations.

| Existing control or information | Responsive treatment |
| --- | --- |
| Site menu glyph, YAVCHN, Go, Applets, Feed, View, Window, Help | Retained with PUDL's existing collapsed menu behavior. |
| Reader, Story, Discussion menus | Retained; View also receives the active reader's three pane choices. |
| Windowed / Classic selector | Retained above 420px; both choices remain in View at narrower widths. |
| Hacker News, Lobsters, Pinned count, Find | Retained in the real feed selector and Feed menu. |
| Feed refresh, Hottest / Active / Newest and source-specific categories | Retained. Search and its submit button remain available on feeds that support search. |
| Story-row pin, read marker, title, rank, host, points, author, age, comment count | Retained. Text continues to wrap rather than hiding story titles. |
| Story-row open in new reader and hide | Retained as equal 28px buttons on desktop and mobile. Hidden-story recovery remains below the list. |
| Sidebar divider | Retained at wide widths; mobile still uses the full-width story list. |
| Window source favicon, title, active-window indication | Retained; narrow titles truncate visually while retaining their accessible names. |
| Window menu, open as page, minimize, maximize/restore, close | All remain in the window menu. Wide readers retain the title-bar buttons; compact readers show the menu button. |
| Stories navigation | Available from the sidebar toggle at the left end of the taskbar. |
| Article label and source domain | The pane selector replaces the repeated label. The domain remains in the article's scrolling metadata row. |
| Open original and article refresh | Retained beside the domain inside the article scroll area, and in Story. Reader failure and archive lookup actions remain in the article content. |
| Next story | Kept at the fixed right edge of the new reading toolbar. It remains visible but disabled when no next story exists; standalone pages omit it. Go retains its navigation command. |
| Article/discussion splitter | Available in Split mode, with an 18px non-overlapping target at every viewport size and for every pointer type. Keyboard resizing remains supported. |
| Discussion count, points, author/profile link, age | Retained inside the discussion's scrolling header, wrapping on narrow readers. |
| Best / Newest / Oldest and source discussion link | Retained inside that header and in the existing menus. |
| Comment collapse controls, author links, ages, permalinks and nested replies | Retained. Comment navigation reveals Discussion if Article alone was visible. |
| Taskbar window buttons, source icons and titles | Retained in a full-width workspace row, visible from the mobile story list. Overflow arrows provide access without relying on transient scrollbars. |
| Taskbar minimize all / restore all and close all | Retained, including the existing state-dependent minimize/restore choice. Window also retains these commands. |

Each reader preserves its pane selection and split ratio through article changes. Browser-local continuity stores those values alongside independent article and discussion scroll positions. Open as a page and Copy story link include the chosen pane and ratio in the story URL; standalone pages read those parameters. Workspace window layout and article assignments continue to use their existing URL representation. Resizing does not change an explicit pane choice. Without JavaScript, both panes and their original/source links remain available.

This implementation uses public PUDL markup, window commands, split properties and events. No vendor files are changed. The outstanding upstream proposal still includes menu height and overflow cues, and should evaluate standard compact chrome and touch-target conventions using this local implementation as evidence.

The accepted layout retains its current control styling. Sidebar controls remain at normal contrast on idle rows. Pin sits above open-in-window in a left-side column with equal 28px buttons and an 8px gap. Hide stays on the right to separate it from the opening action. Pin, open, and hide remain equal at 28px on mobile as well as desktop. Pinned stories retain their orange pressed state, and each action has an accessible label and tooltip. All segmented selectors and their compact menu buttons use PUDL's standard component styling, including feed filters, reader panes, comment ordering, and Settings. Each selector retains its existing dimensions.

Menu bars and selector tracks follow the active theme. Light mode uses a light gray background with dark text; dark mode uses a slightly darker graphite background. The palettes are not exchanged between modes. Raised selected states and control dimensions remain unchanged.

The editable base palettes are exported in `src/static/yavchn-theme.css` for PUDL Theme Studio. Studio edits those palette tokens. PUDL derives menu and selector styling from them, matching the Studio previews; the site supplies layout and sizing.

The Theme Studio comparison supersedes the custom selector tracks and derived top-bar overrides. Those overrides have been removed so the exported palette produces the same standard PUDL control appearance in YAVCHN.

In light mode, the top bar uses the Settings surface color behind its selectors so their recessed tracks remain distinct. This background adjustment does not change selector colors or the dark-mode top bar.

Menu-bar groups use the selector track background (`--recess-bg`) with PUDL raised borders and shadows in both themes. The border replaces one pixel of padding to preserve their size. Menu items, open-menu states, and dropdown panels keep their existing styling.

The taskbar begins with a sidebar glyph button. On wide layouts it shares the View menu and F-key sidebar visibility setting. At widths of 640px or less it switches between the list and reader workspace using PUDL window minimization and restoration, preserving mounted readers and restoring the previously visible windows with the active one last. Previously minimized windows stay minimized. The glyph shows a filled sidebar when visible and an outline when hidden; its tooltip and accessible state describe the current action. The mobile title-bar back link is removed. With no open windows on mobile, the toggle remains disabled until a reader is opened.

Narrow readers must expose only maximized and minimized placement states, with Restore to floating disabled. PUDL 0.40.0 has no documented host capability restriction covering its menus, gestures, and script actions. That runtime support remains pending; the larger 20px window-menu chevron is implemented locally.
