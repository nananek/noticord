# noticord

Discord 互換の Webhook URL を払い出し、届いた通知を **PWA に Web Push** するシングルユーザー専用の軽量アプリ。
各種サービス(Grafana / Uptime Kuma / GitHub / アラートマネージャ 等)の「Discord Webhook」送信先に
noticord の URL を貼るだけで、自分のスマホ/PC に OS 通知が飛びます。embed は Discord 風にレンダリングして履歴表示します。

## 構成(3コンテナ)

```
            tailnet (HTTPS, MagicDNS)
                     │
   ┌─────────────────┴─────────────────┐
   │   tailscale  ──netns──  admin      │   admin = 中核
   └───────────────────────────┬────────┘   ・PWA配信/管理API … tailscale netns 上の TCP(127.0.0.1:8080)
                               │            ・Discord互換受信 … UDS /run/noticord/ingest.sock
                  /run/noticord/ingest.sock     ・DB / VAPID / Web Push 送信を専有
                               │
   ┌───────────────────────────┴────────┐
   │           cloudflared              │   公開URL → unix:/run/noticord/ingest.sock
   └────────────────────────────────────┘
              Cloudflare Tunnel(egress)
```

- **アプリ間のブリッジネットワークは無し**。admin と cloudflared の唯一の接点は **UDS 1本**。
- admin の UDS サーバーには受信ルート(`/api/webhooks/*`)しか載っていないため、
  仮に cloudflared が乗っ取られても **通知の投函しかできず**、管理 API・履歴・購読情報には到達できません。
- Web Push 送信は egress を持つ admin 側が行うので、**cloudflared 以外は外向き通信を持ちません**。
- 自作コンテナ(admin)は `gcr.io/distroless/static:nonroot` で **実体 ~9.5MB(イメージ ~20MB)/ cgo なし / 非 root(uid 65532)**。

| サービス | イメージ | 役割 |
|---|---|---|
| tailscale | `tailscale/tailscale` | admin を tailnet に HTTPS 公開(serve) |
| admin | distroless/static:nonroot(自作) | PWA・管理API・受信・DB・Push |
| cloudflared | `cloudflare/cloudflared` | Discord 互換 URL を公開し UDS へ直結 |

## 必要なもの

- Docker / Docker Compose
- Tailscale アカウント(admin 画面への私的アクセス用)
- Cloudflare アカウント + 任意のドメイン(Webhook の公開 URL 用)

## セットアップ

### 1. データ/ソケット用ディレクトリの準備

bind mount するディレクトリを、コンテナ実行ユーザー(distroless nonroot = **uid 65532**)が書けるようにします。

```bash
mkdir -p data run
sudo chown -R 65532:65532 data run
```

### 2. Tailscale 認証キー

[Tailscale admin > Settings > Keys](https://login.tailscale.com/admin/settings/keys) で Auth key を発行
(Reusable / Ephemeral 推奨)。tailnet で **HTTPS / MagicDNS** を有効化しておくこと
(serve が `${TS_CERT_DOMAIN}` の証明書を使うため)。

### 3. Cloudflare Tunnel

1. Zero Trust > Networks > Tunnels で **Tunnel を作成**し、トークン(`eyJ...`)を控える。
2. その Tunnel に **Public Hostname** を追加:
   - Subdomain/Domain: 例 `noticord.example.com`
   - **Service Type: `Unix Socket`**
   - **URL: `/run/noticord/ingest.sock`**

> ダッシュボードに `Unix Socket` が無い場合は locally-managed 構成を使ってください
> (`config.yml` に `service: unix:/run/noticord/ingest.sock`)。

### 4. .env

```bash
cp .env.example .env
$EDITOR .env
```

| 変数 | 説明 |
|---|---|
| `TS_AUTHKEY` | Tailscale Auth key |
| `TUNNEL_TOKEN` | Cloudflare Tunnel トークン |
| `NOTICORD_PUBLIC_URL` | 公開 URL(例 `https://noticord.example.com`)。管理画面の Webhook URL 表示に使用 |
| `NOTICORD_ADMIN_PASSWORD` | 管理画面のログインパスワード。**空にすると認証なし**(Tailscale 網のみで保護) |
| `NOTICORD_VAPID_SUBJECT` | VAPID 連絡先(`mailto:you@example.com`) |

### 5. 起動

```bash
docker compose up -d --build
```

- 管理画面: tailnet 上の `https://noticord.<your-tailnet>.ts.net/`(MagicDNS 名は `tailscale status` で確認)
- Webhook URL: 管理画面の「Webhook URL」で発行 → コピーして各サービスへ

## 使い方

1. tailnet 経由で管理画面を開き(必要ならログイン)、**ホーム画面に追加**して PWA 化。
2. 「このデバイスで通知を有効化」→ 通知許可。複数デバイスで有効化すれば全部に届きます。
3. 「URL を発行」で Discord 互換 URL を作成し、送信元サービスの Discord Webhook 先に貼り付け。
4. 届いた通知は OS 通知 + 履歴に表示。embed(title / description / fields / image / footer / 色 / 簡易 Markdown)を整形表示します。

### Discord 互換について

`POST /api/webhooks/{id}/{token}` で以下を受けます:

- `application/json`(`content`, `username`, `avatar_url`, `embeds[]`)
- `application/x-www-form-urlencoded` / `multipart/form-data` の `payload_json`
- `?wait=true`(メッセージオブジェクトを返却。既定は `204 No Content`)

embed は `title / description / url / color / author / fields[] / image / thumbnail / footer / timestamp` を保持。
OS 通知はプレーンテキストのため要約し、履歴ビューでフル装飾します(簡易 Markdown: `**太字**` `*斜体*` `__下線__` `~~取消~~` `` `コード` `` `[text](url)`)。

動作確認(tailnet 内 or 公開 URL):

```bash
curl -X POST "$NOTICORD_PUBLIC_URL/api/webhooks/<id>/<token>" \
  -H 'Content-Type: application/json' \
  -d '{"username":"test","embeds":[{"title":"hello","description":"**bold** and `code`","color":5814783}]}'
```

## 運用メモ

- **データ**: すべて `./data/noticord.db`(SQLite, WAL)。バックアップはこのファイルを退避するだけ。
- **履歴保持**: 既定で最新 1000 件(`internal/config` の `KeepMessages`)。
- **VAPID 鍵**: admin 初回起動時に生成し DB に保存。鍵を失うと既存購読は無効化されるため、`./data` は保持すること。
- **iOS**: PWA をホーム画面に追加した状態でのみ Web Push が有効(iOS 16.4+)。
- **アイコン**: `web/static/icon.svg`。PNG にしたい場合は差し替えて `web/static/manifest.json` を更新。

## 開発

```bash
go run ./cmd/admin     # NOTICORD_LISTEN=:8080 などを環境変数で
docker build -f Dockerfile.admin -t noticord-admin .
```

ディレクトリ:

```
cmd/admin          中核プロセス(PWA配信+管理API+UDS受信)
internal/db        SQLite(modernc, 純Go)・トークン/購読/履歴/VAPID
internal/ingest    Discord互換受信ハンドラ(UDSサーバーに搭載)
internal/discord   embed を含むペイロード解釈
internal/push      Web Push(VAPID)送信
internal/config    環境変数
web/static         PWA(index.html / app.js / sw.js / manifest / icon)
```
