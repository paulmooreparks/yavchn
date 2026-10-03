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

## Synchronization contract

Account data is private and available at `/account/data`. Its representation contains pinned stories, blocked domains, and retained applet reading state. Writes require the current representation's ETag through If-Match; a stale write is rejected rather than replacing newer data. Browser clients rebase changes to individual entries on the returned latest representation before retrying. An explicit edit to the same entry is applied after the remote change.

The browser uses account-specific storage keys for synchronized data. Anonymous data remains separate and is imported only after an explicit action. Signing out returns to anonymous storage. Requests from an old tab must not update another account after the session changes.

The account page is addressable at `/account`. Export, sign-out, other-session revocation, and account deletion are available there. Deletion requires a sign-in within the preceding ten minutes. GitHub callback transactions expire after ten minutes and are consumed once. Account sessions expire after thirty days.
