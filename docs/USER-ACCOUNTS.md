# YAVCHN user accounts

Paul approved optional YAVCHN accounts on 2026-10-03. HN and Lobsters login and credential storage are deferred. The architectural rules remain defined in `C:\Users\paul\OneDrive\Documents\Architectural Principles.md`.

## Authentication

The first login provider will be GitHub, using its documented authorization-code flow with PKCE and state validation. YAVCHN will request only the access needed to identify the user and discard the provider token after identification. Each account will have an internal identifier independent of its login provider. Additional providers must be linked explicitly rather than merged by matching email addresses.

YAVCHN will use revocable server-side sessions with host-only Secure and HttpOnly cookies. State-changing requests will require protection against cross-site request forgery. Authentication and private account responses must not enter shared caches. Sessions and private data must remain isolated between production and beta.

Passkeys are a later authentication option. Recovery must be designed before they are introduced.

## Account features

The initial account features will synchronize pinned stories, blocked domains, and reading progress. Anonymous use will remain available. On first sign-in, users will be offered an explicit import of this browser's existing data. Signing out must prevent another account from inheriting synchronized private state.

Account export, deletion, and revocation of other sessions belong in the initial account release. Window geometry will remain device-specific. Later features may include background reply notifications, saved searches, collections and notes, and saved workspaces.

Source-site participation will continue through the existing links to HN and Lobsters. YAVCHN will not collect their passwords or session cookies, and account ownership verification is outside the initial scope.

## Beta deployment

The work branch is `feature/user-accounts`. Run `./build.ps1 -Target Beta` to deploy its Docker image as `yavchn-beta` on `127.0.0.1:8087`. Its persistent database is stored in `yavchn-beta-data`, separately from production's `yavchn-data` volume. Production remains on port 8086 and requires the `main` branch when deployed through the script.

The Windows Cloudflared service routes `beta.yavchn.com` to `http://127.0.0.1:8087`. Paul will configure the hostname in Cloudflare. The service's active configuration is `C:\Windows\System32\config\systemprofile\.cloudflared\config.yml`.

The beta origin and ingress rule were verified on 2026-10-03. Reloading the new ingress rule requires `Restart-Service Cloudflared` in an elevated PowerShell session. The development session could edit and validate the configuration but lacked permission to restart that service. Paul completed the domain setup and service reload. The beta hostname is now beta.yavchn.com.

Before authentication can be exercised, a separate GitHub OAuth application must be registered for the beta site. Its homepage will be `https://beta.yavchn.com/` and its callback will be `https://beta.yavchn.com/auth/github/callback`. The client secret must be supplied through deployment configuration outside the repository. Production will use a separate registration.

## Production domain

Paul selected `yavchn.com` as the production domain for user accounts on 2026-10-03. The tunnel configuration routes `yavchn.com` and `www.yavchn.com` to the existing production container on port 8086. The original `yavchn.parkscomputing.com` route remains available. Login development continues on `beta.yavchn.com`, with its separate container and database.

The production GitHub OAuth callback will be `https://yavchn.com/auth/github/callback`. Production sessions must use host-only cookies on `yavchn.com`. Authentication return URLs must not be derived from an unvalidated request hostname.

Paul will create proxied CNAME records for `@` and `www` in the `yavchn.com` Cloudflare zone, targeting `b2594007-2b75-4103-bfbc-f54f7754f62a.cfargotunnel.com`. The zone must belong to the same Cloudflare account as the existing tunnel. These DNS records and the local ingress configuration are separate requirements.

A redirect from the original hostname remains a proposal. Browser-local pins, reading progress, and preferences cannot be read by the new origin. A migration path must precede a forced redirect so users can retain their existing data. The proposed transition offers an explicit transfer from the original site, then introduces a redirect preserving the path and query string after the new domain and migration have been verified. Installed web apps also belong to their original origin and need separate migration guidance.

## Top bar and profile picture

The account sits at the right end of the top bar, as on parkscomputing.com. A reader who is not signed in sees a Sign in pill when the deployment offers GitHub sign-in. It opens the Account applet, where the GitHub button starts sign-in, because that form needs the token the account page issues. A signed-in reader sees their picture in a round pill that opens their account, or a head-and-shoulders placeholder when there is no picture.

The picture is the reader's GitHub avatar. At each sign-in the server fetches it at 96 pixels from `avatars.githubusercontent.com`, without the provider token, and stores a copy of at most 256 KB when its bytes are PNG, JPEG, GIF, or WebP. Pages show the copy from `/account/avatar`, so a reader's browser never contacts GitHub to draw it. The copy is served only to its owner, at an address that changes with the picture, and may be kept only in a private cache. A failed fetch keeps the previous copy and never stops sign-in. Deleting the account deletes the copy. The export leaves it out, since it is GitHub's public picture and not data the reader created.

## Collections and notes

Paul approved collections and notes on 2026-10-04 as the first features after the initial account release. Both work anonymously in browser storage, as pins do, and synchronize when the reader is signed in.

A collection is a named list of stories. A story can belong to any number of collections, and pins remain a separate list. The Story menu's Add to collection submenu files the front story into an existing collection or a new one, and shows a check beside each collection that already holds it. The Collections feed at `/collections/` lists every collected story. Repeated `c` parameters choose one or more collections, and the address of the first form, `/collections/{id}/`, redirects to the same view with that collection chosen. A menu in the list bar holds a checkbox for each collection, and its button names what is shown. The same menu ends with New collection, and with Rename and Delete when exactly one collection is chosen. Those commands sit in the list bar, not the front menu, because a story window in front owns the front menu. Deleting a collection removes its entries and leaves the stories in other collections untouched.

Pinned, Collections, and Notes share their filters, after the category and tag menus on parkscomputing.com (Paul, 2026-10-04). The words box comes first. A Show menu follows it, with a checkbox for each site and one for stories the reader has not opened, so choosing both sites or neither shows every site. An Order menu names the current order and lists the others. Each checkbox belongs to the filter form, so ticking boxes and pressing the form's button works without script. With script, each tick applies at once, and the menu reopens after the list bar is swapped in, so the reader can tick several boxes in a row. Every filter in force appears as a chip that removes it.

A note is private text that the reader attaches to a story. The Note button in a story's toolbar opens an editor above the article and discussion, and the Story menu opens the same editor. The button is a disclosure whose 16-pixel SVG chevron turns over as the editor opens and closes. Each story remembers whether the reader left its editor open or closed, with the rest of its window state, and a story the reader has not yet chosen for opens the editor when it has a note. The note saves as the reader types, and clearing its text deletes it. Story rows in every feed show a note glyph when the story has a note. The Notes feed at `/notes/` lists every noted story with the start of its note, and its words filter also searches the note text. A note holds at most 10,000 characters, and an account holds at most 1,000 notes, 100 collections, and 2,000 collection entries.

The top bar's source switch had no room for two more entries, so its Pinned entry became Saved. Saved stays selected on all three views, and their list bar shows Pinned, Collections, and Notes as tabs, the way Hacker News shows its lists. The pin count moved to the Pinned tab.

Collection identifiers are random lowercase strings generated in the browser, so a collection created offline needs no server round trip. Collection entries and notes each keep a copy of the story's title, address, site, author, points, and comment count, so the feeds can list a story after it has left its source's lists. Collection and note names appear only in the browser. The server renders an empty list and the browser fills it from storage, as it does for Pinned.

## Synchronization contract

Account data is private and available at `/account/data`. Its representation contains pinned stories, blocked domains, retained applet reading state, collections, collection entries, and notes. Collections are keyed by identifier. Collection entries are keyed by the collection identifier and the story key, joined by a colon, so two devices adding different stories to one collection merge without conflict. Notes are keyed by story key. When two devices edit the same note, the edit synchronized last replaces the other, as with every other entry. The server rejects a collection entry whose collection does not exist, and browsers drop such entries after merging.

A write that omits the collections, collection entries, or notes member keeps the stored value of that member. A tab still running a script from before these features therefore cannot erase them. The other members remain required.

Writes require the current representation's ETag through If-Match; a stale write is rejected rather than replacing newer data. Browser clients rebase changes to individual entries on the returned latest representation before retrying. An explicit edit to the same entry is applied after the remote change.

The browser uses account-specific storage keys for synchronized data. Anonymous data remains separate and is imported only after an explicit action. Signing out returns to anonymous storage. Requests from an old tab must not update another account after the session changes.

The Account applet uses the same content in a PUDL window and its Classic page at `/account?view=classic`. The site menu opens it in the current Windowed workspace without replacing existing readers. Its identity menu supports opening it as a page and copying its link. Sign-in and account-action returns preserve the workspace that submitted them; the server accepts only local account or known Windowed feed destinations. Export, sign-out, other-session revocation, and account deletion are available there. Deletion requires a sign-in within the preceding ten minutes. GitHub callback transactions expire after ten minutes and are consumed once. Account sessions expire after thirty days.
