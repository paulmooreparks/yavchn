# Reader windows

Reader windows have stable instance keys such as `reader-1`. The `r.<window-key>` query parameter names the article displayed in that instance. PUDL continues to own placement, minimization and stacking. Reload and browser history restore both the arrangement and each instance's article. Existing article-key URLs remain supported.

The Window menu starts with New reader window, which opens an empty floating reader. Applets follows Window and precedes Help. An article row offers Open in new reader window, which creates a floating reader containing that article.

Sidebar navigation uses the most recently active undocked reader for the article's source, preferring a visible reader and otherwise restoring a minimized reader. An empty reader can take any source and acquires that source when an article is selected. Navigation creates a reader when none qualifies. Switching between HN and Lobsters therefore preserves a reader for each site by default. Docking protects an article from sidebar navigation. Minimizing a reader does not prevent reuse; the mobile Stories action minimizes readers to reveal the feed. Next story remains an explicit action on its own reader, including docked readers; it can change the reader's source when navigating a mixed list.

Each instance remembers its position within each article independently. Reader assignments are carried across list changes. Closing a reader removes its assignment from the current URL. The server renders empty readers and article assignments without JavaScript; links to create readers preserve the current list and windows.

The bundled PUDL regions script recognizes YAVCHN's `r.` parameters as persistent desktop state. Collapsed segment-menu links within regions use the same navigation as their desktop counterparts, preserving open readers when the source changes on mobile. The bundled windows script drops assignments for closed windows when writing its URL. These host integration changes must be retained when updating PUDL.

The cross-project principles remain defined in `C:\Users\paul\OneDrive\Documents\Architectural Principles.md`.

Reader title bars show the source favicon before the article title. The source name is available to screen readers without visible site text. Task-bar entries repeat the icon and label, including minimized readers. The source selector immediately to the right of the menus shows each site's favicon beside its name, a pin glyph beside Pinned, and a search glyph beside Find. The narrow-screen selector keeps these icons. These are local copies of the icons declared by the sites: HN's `https://news.ycombinator.com/y18.svg` and Lobsters' `https://lobste.rs/touch-icon-144.png`. Empty readers have no source marker. The marker follows the displayed article across navigation and browser history.

## Feed refresh

The Feed menu sits between View and Window. Its Refresh feed command uses the same action as the looped-arrow toolbar button and is disabled when no feed is available or a refresh is running. The looped-arrow button at the start of the feed toolbar refreshes the current HN or Lobsters list, including the current list tab, page, or HN search. PUDL's host extension `pudlRegions.reload()` revalidates and replaces the current regions without replacing reader windows or adding history. Navigation cancels an outstanding refresh. Failure leaves the previous feed in place and exposes a retry message. The request uses `Cache-Control: no-cache`; source feeds refetch their IDs and refresh or invalidate their item summaries. A refresh replaces any pages appended by infinite scrolling with the page named in the URL.

## Archive lookup after reader failures

The reader preserves upstream HTTP status codes and displays them when extraction fails. Responses 401, 403, 404, 410 and 451 offer a secondary "Look for an archived copy" link alongside "Open the article". The link opens the Internet Archive Wayback calendar for the original article URL. It does not assert that a capture exists or submit the page for archiving. Other failures retain the original-page action alone. Archive URLs preserve the article query but omit credentials and fragments.

The calendar URL format is documented in the [Internet Archive Wayback CDX documentation](https://github.com/internetarchive/wayback/blob/master/wayback-cdx-server/README.md#filtering).

The article toolbar places Open original before Refresh, with Next story at the far right. Next story retains its position when a story has no external article.

## Window and Classic views

Window and Classic are explicit browsing views, independent of viewport width. Feed URLs accept `view=window` or `view=classic`. An explicit view takes priority over the browser preference. An address that names open windows implies Window when no view is specified. Other feed addresses use the saved preference, defaulting to Window. Standalone story and applet routes always render Classic pages, including shared links opened by someone whose preference is Window.

View menu choices submit to `POST /settings/view`, save the preference in the browser's `yavchn-view` cookie, and navigate to the selected view. Switching from a window to Classic keeps the active article or applet as a page. Switching a standalone page to Window opens that content in the desktop. One-off Open as a page and Open in a window links do not modify the preference. Classic feeds use the full content width, navigate to story pages, and provide a return link from each story to its source feed.

Menu visibility uses a server-rendered page context, borrowing Parks Computing's server-side `when` filtering approach without introducing its configuration format. Standalone pages omit Feed, Window, and the story-list visibility command. Classic feeds retain Feed controls. Theme and Domain filters apply across views; Applets links open standalone pages outside the desktop. Story pages omit Next story and Close. Discussion commands are disabled without comments, and article refresh is disabled while fetching. Keyboard shortcuts use the same contextual restrictions as menu commands. No window or classic mode is inferred from screen width.

The feed selector and its matching YAVCHN menu entries appear only on stories views, including Classic feeds, Pinned, and Find. Standalone Classic article and applet pages omit both the selector and its divider. Articles retain their Back to stories link.
