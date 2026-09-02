**English** | [日本語](README.ja.md)

# noticord

A lightweight, single-user app that issues **Discord-compatible Webhook URLs** and delivers incoming
notifications to a **PWA via Web Push**. Just paste a noticord URL into the "Discord Webhook" target of any
service (Grafana / Uptime Kuma / GitHub / Alertmanager, etc.) and OS notifications land on your phone/PC.
Embeds are rendered Discord-style in the history view.

## Architecture (3 containers)

```
            tailnet (HTTPS, MagicDNS)
                     │
   ┌─────────────────┴─────────────────┐
   │   tailscale  ──netns──  admin      │   admin = the core
   └───────────────────────────┬────────┘   - PWA / admin API … TCP in the tailscale netns (127.0.0.1:8080)
                               │            - Discord-compatible ingest … UDS /run/noticord/ingest.sock
                  /run/noticord/ingest.sock     - owns DB / VAPID / Web Push sending
                               │
   ┌───────────────────────────┴────────┐
   │           cloudflared              │   public URL → unix:/run/noticord/ingest.sock
   └────────────────────────────────────┘
              Cloudflare Tunnel (egress)
```

- **No bridge network between containers.** The only contact point between admin and cloudflared is **a single UDS**.
- The admin UDS server only exposes the ingest route (`/api/webhooks/*`), so even if cloudflared were
  compromised it could **only post notifications** — it cannot reach the admin API, history, or subscriptions.
- Web Push sending is done by admin (which has egress), so **nothing but cloudflared has outbound connectivity**.
- The custom container (admin) is built on `gcr.io/distroless/static:nonroot`: **~9.5MB binary (~20MB image) / no cgo / non-root (uid 65532)**.

| Service | Image | Role |
|---|---|---|
| tailscale | `tailscale/tailscale` | Expose admin over HTTPS on the tailnet (serve) |
| admin | distroless/static:nonroot (custom) | PWA / admin API / ingest / DB / Push |
| cloudflared | `cloudflare/cloudflared` | Publish the Discord-compatible URL straight to the UDS |

## Requirements

- Docker / Docker Compose
- A Tailscale account (for private access to the admin screen)
- A Cloudflare account + any domain (for the public Webhook URL)

## Setup

### 1. Prepare the data/socket directories

Make the bind-mounted directories writable by the container's run user (distroless nonroot = **uid 65532**).

```bash
mkdir -p data run
sudo chown -R 65532:65532 data run
```

### 2. Tailscale auth key

Issue an Auth key at [Tailscale admin > Settings > Keys](https://login.tailscale.com/admin/settings/keys)
(Reusable / Ephemeral recommended). Enable **HTTPS / MagicDNS** on your tailnet
(serve uses the `${TS_CERT_DOMAIN}` certificate).

### 3. Cloudflare Tunnel

1. Under Zero Trust > Networks > Tunnels, **create a Tunnel** and note its token (`eyJ...`).
2. Add a **Public Hostname** to that Tunnel:
   - Subdomain/Domain: e.g. `noticord.example.com`
   - **Service Type: `Unix Socket`**
   - **URL: `/run/noticord/ingest.sock`**

> If `Unix Socket` is missing from the dashboard, use a locally-managed config
> (`service: unix:/run/noticord/ingest.sock` in `config.yml`).

### 4. .env

```bash
cp .env.example .env
$EDITOR .env
```

| Variable | Description |
|---|---|
| `TS_AUTHKEY` | Tailscale auth key |
| `TUNNEL_TOKEN` | Cloudflare Tunnel token |
| `NOTICORD_PUBLIC_URL` | Public URL (e.g. `https://noticord.example.com`). Used to display the Webhook URL in the admin screen |
| `NOTICORD_ADMIN_PASSWORD` | Admin login password. **Leave empty for no auth** (protected by the Tailscale network only) |
| `NOTICORD_VAPID_SUBJECT` | VAPID contact (`mailto:you@example.com`) |
| `NOTICORD_LANG` | UI language. `en` or `ja` (unset or unknown → `en`) |

### 5. Start

```bash
docker compose up -d --build
```

- Admin screen: `https://noticord.<your-tailnet>.ts.net/` on the tailnet (find the MagicDNS name with `tailscale status`)
- Webhook URL: issue one via "Webhook URL" in the admin screen → copy it into each service

## Usage

1. Open the admin screen over the tailnet (log in if required) and **Add to Home Screen** to install the PWA.
2. Open **🔔 Notifications** at the bottom of the sidebar and tap **"Enable notifications"** → grant permission.
   Enable it on multiple devices to receive on all of them. The same panel also lets you send a ✈️ test
   notification and toggle the 📢 notification sound on/off.
3. Create a Discord-compatible URL with "Issue URL" and paste it into the Discord Webhook target of the sending service.
4. Incoming notifications appear as OS notifications + in the history. Embeds (title / description / fields /
   image / footer / color / basic Markdown) are rendered.

### Discord compatibility

`POST /api/webhooks/{id}/{token}` accepts:

- `application/json` (`content`, `username`, `avatar_url`, `embeds[]`)
- `payload_json` in `application/x-www-form-urlencoded` / `multipart/form-data`
- Images in multipart `files[n]` (`image/jpeg`, `image/png`, `image/gif`, or `image/webp`; 10 MiB for the complete request)
- `?wait=true` (returns the message object; default is `204 No Content`)

Embeds preserve `title / description / url / color / author / fields[] / image / thumbnail / footer / timestamp`.
OS notifications are plain text so they are summarized, while the history view shows full decoration
(basic Markdown: `**bold**` `*italic*` `__underline__` `~~strikethrough~~` `` `code` `` `[text](url)`).

Quick test (inside the tailnet or via the public URL):

```bash
curl -X POST "$NOTICORD_PUBLIC_URL/api/webhooks/<id>/<token>" \
  -H 'Content-Type: application/json' \
  -d '{"username":"test","embeds":[{"title":"hello","description":"**bold** and `code`","color":5814783}]}'
```

An image-only message is valid. Use `payload_json` and a uniquely numbered `files[n]` field (where `n` is
an integer from 0 through 2147483647); the optional `attachments` array adds its description. This bounded
index range is separate from the snowflake-shaped IDs returned for stored attachments, so PATCH can retain an
existing image while adding any valid `files[n]`. Images are stored in the SQLite history and displayed only
after logging in to the private admin screen (the public webhook endpoint never serves image bytes).

```bash
curl -X POST "$WEBHOOK_URL?wait=true" \
  -F 'payload_json={"embeds":[{"image":{"url":"attachment://graph.png"}}],"attachments":[{"id":0,"filename":"graph.png","description":"Daily graph"}]}' \
  -F 'files[0]=@graph.png;type=image/png'
```

### Editing / deleting messages (Discord-compatible)

Just like Discord webhooks, you can edit or delete later using the `id` (message_id) returned with `?wait=true`.
The URL is simply the issued Webhook URL plus `/messages/<message_id>`.

```bash
# Send and capture the message_id
MID=$(curl -s -X POST "$WEBHOOK_URL?wait=true" \
  -H 'Content-Type: application/json' \
  -d '{"content":"Working…"}' | jq -r .id)

# Edit (replaces content/embeds; no notification is re-sent)
curl -X PATCH "$WEBHOOK_URL/messages/$MID" \
  -H 'Content-Type: application/json' \
  -d '{"content":"✅ Done"}'

# Get / delete
curl "$WEBHOOK_URL/messages/$MID"
curl -X DELETE "$WEBHOOK_URL/messages/$MID"
```

- `PATCH` … fully replaces `content` / `embeds` and attachments. To retain an existing image, include its returned attachment `id` in `attachments`; include `files[n]` to add an image. Omitting `attachments` removes existing images. `username` / `avatar_url` are updated only when provided. Returns `200` with the message.
- `GET` … returns the message object.
- `DELETE` … `204 No Content`.
- Edits/deletes are reflected **in place via SSE** on the open screen (edits don't re-notify, same as Discord).
- You cannot edit/delete messages sent by another webhook (`404 Unknown Message`).

## Operations notes

- **Data**: everything, including accepted image bytes, lives in `./data/noticord.db` (SQLite, WAL). Backup = just copy that file. Image storage grows with retained history.
- **History retention**: latest 1000 messages by default (`KeepMessages` in `internal/config`).
- **VAPID keys**: generated on admin's first start and stored in the DB. Losing the keys invalidates existing
  subscriptions, so keep `./data`.
- **UI language**: choose `en` / `ja` with `NOTICORD_LANG` (default `en`). It is fixed at server start and
  passed to the PWA via `/api/me`.
- **iOS**: Web Push works only when the PWA is added to the Home Screen (iOS 16.4+).
- **Icon**: `web/static/icon.svg`. To use a PNG, replace it and update `web/static/manifest.json`.

## Development

```bash
go run ./cmd/admin     # set NOTICORD_LISTEN=:8080 etc. via env vars
docker build -f Dockerfile.admin -t noticord-admin .
```

Directories:

```
cmd/admin          core process (PWA serving + admin API + UDS ingest)
internal/db        SQLite (modernc, pure Go) · tokens/subscriptions/history/VAPID
internal/ingest    Discord-compatible ingest handler (mounted on the UDS server)
internal/discord   payload parsing incl. embeds
internal/push      Web Push (VAPID) sending
internal/config    environment variables
web/static         PWA (index.html / app.js / sw.js / manifest / icon)
```

UI strings are centralized in the `I18N` object (`en` / `ja`) at the top of `web/static/app.js`. In HTML,
translatable targets are marked with `data-i18n` / `data-i18n-ph` / `data-i18n-title` attributes and swapped in at startup.
