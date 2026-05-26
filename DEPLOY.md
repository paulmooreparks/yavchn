# Deploying yavchn

Target deployment: self-hosted on the Windows + WSL2 workstation, fronted by
the existing cloudflared tunnel. Public URL: `https://yavchn.parkscomputing.com`.

## 1. Build and run the container

```
docker build -t yavchn:dev .
docker rm -f yavchn 2>/dev/null
docker run -d \
  --name yavchn \
  --restart unless-stopped \
  -p 127.0.0.1:8086:8080 \
  yavchn:dev
```

The container binds to `127.0.0.1` only — never directly exposed to the LAN
or public internet. All public traffic arrives via cloudflared.

## 2. Wire up cloudflared

The tunnel already exists (`b2594007-2b75-4103-bfbc-f54f7754f62a`). Two steps:

### 2a. Add the DNS route

```
cloudflared tunnel route dns b2594007-2b75-4103-bfbc-f54f7754f62a yavchn.parkscomputing.com
```

This creates a CNAME from `yavchn.parkscomputing.com` to
`<tunnel-uuid>.cfargotunnel.com`. One-time, idempotent.

### 2b. Add the ingress rule

Edit `C:\Windows\System32\config\systemprofile\.cloudflared\config.yml` (admin
required) and insert this entry under `ingress:` before the catch-all `404`
rule:

```yaml
  # yavchn
  - hostname: yavchn.parkscomputing.com
    service: http://127.0.0.1:8086
```

Then restart the cloudflared service (PowerShell admin):

```powershell
Restart-Service cloudflared
```

## 3. Smoke test

```
curl -I https://yavchn.parkscomputing.com/
curl -s https://yavchn.parkscomputing.com/ | head -20
```

Expect `200 OK` and the story list HTML.

## Updating

After a `git pull`:

```
docker build -t yavchn:dev .
docker rm -f yavchn
docker run -d --name yavchn --restart unless-stopped -p 127.0.0.1:8086:8080 yavchn:dev
```

The SQLite article cache lives at `/home/nonroot/yavchn.db` inside the
container, in the container's writable layer. Cache is rebuilt on first
fetch after a container replace — acceptable for an article-extraction
cache (it just refills as users click stories).

If cache persistence across rebuilds becomes useful: add a named volume.

```
docker volume create yavchn-data
docker run -d \
  --name yavchn \
  --restart unless-stopped \
  -p 127.0.0.1:8086:8080 \
  -v yavchn-data:/home/nonroot \
  yavchn:dev
```

## Sizing notes

The 128 GB workstation is comfortably oversized. Concrete caps in the code:

- **Item cache:** 2048 entries (LRU + 5min TTL) ≈ 2-4 MB
- **Thread cache:** 256 entries (LRU + 3min TTL) ≈ up to 50-100 MB on a busy day
- **Outbound article extraction:** 20 concurrent fetches max (semaphore)
- **Per-host connections to firebaseio/algolia:** 30 max
- **Top-stories list:** refreshed every 60s in a background goroutine

Worst-case steady-state RAM under HN-front-page load: ~150 MB.
