[English](README.md) | **日本語**

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
| `NOTICORD_LANG` | UI 言語。`en` または `ja`(未設定・未知の値は `en`) |

### 5. 起動

```bash
docker compose up -d --build
```

- 管理画面: tailnet 上の `https://noticord.<your-tailnet>.ts.net/`(MagicDNS 名は `tailscale status` で確認)
- Webhook URL: 管理画面の「Webhook URL」で発行 → コピーして各サービスへ

## 使い方

1. tailnet 経由で管理画面を開き(必要ならログイン)、**ホーム画面に追加**して PWA 化。
2. サイドバー下部の **🔔 通知設定** を開き、**「通知を有効化」**→ 通知許可。複数デバイスで有効化すれば全部に届きます。同じパネルから ✈️ テスト通知の送信や、📢 通知音のオン/オフ切り替えができます。
3. 「URL を発行」で Discord 互換 URL を作成し、送信元サービスの Discord Webhook 先に貼り付け。
4. 届いた通知は OS 通知 + 履歴に表示。embed(title / description / fields / image / footer / 色 / 簡易 Markdown)を整形表示します。

### Discord 互換について

`POST /api/webhooks/{id}/{token}` で以下を受けます:

- `application/json`(`content`, `username`, `avatar_url`, `embeds[]`)
- `application/x-www-form-urlencoded` / `multipart/form-data` の `payload_json`
- multipart の `files[n]` による画像(`image/jpeg` / `image/png` / `image/gif` / `image/webp`、リクエスト全体で 10 MiB まで)
- `?wait=true`(メッセージオブジェクトを返却。既定は `204 No Content`)

embed は `title / description / url / color / author / fields[] / image / thumbnail / footer / timestamp` を保持。
OS 通知はプレーンテキストのため要約し、履歴ビューでフル装飾します(簡易 Markdown: `**太字**` `*斜体*` `__下線__` `~~取消~~` `` `コード` `` `[text](url)`)。

動作確認(tailnet 内 or 公開 URL):

```bash
curl -X POST "$NOTICORD_PUBLIC_URL/api/webhooks/<id>/<token>" \
  -H 'Content-Type: application/json' \
  -d '{"username":"test","embeds":[{"title":"hello","description":"**bold** and `code`","color":5814783}]}'
```

画像だけの投稿も可能です。`payload_json` と一意な `files[n]` を使います(`n` は 0〜2147483647 の整数)。任意の `attachments` 配列で説明を付けられます。この範囲は保存済み添付に返す snowflake 形式 ID と分離されるため、PATCH で既存画像を残したまま任意の有効な `files[n]` を追加できます。画像は SQLite の履歴へ保存され、ログイン済みの管理画面内だけで表示されます(公開 Webhook 側は画像バイトを配信しません)。

```bash
curl -X POST "$WEBHOOK_URL?wait=true" \
  -F 'payload_json={"embeds":[{"image":{"url":"attachment://graph.png"}}],"attachments":[{"id":0,"filename":"graph.png","description":"日次グラフ"}]}' \
  -F 'files[0]=@graph.png;type=image/png'
```

### メッセージの編集・削除(Discord 互換)

Discord の Webhook と同じく、`?wait=true` で受け取った `id`(message_id)を使って後から編集・削除できます。
URL は発行された Webhook URL に `/messages/<message_id>` を付けるだけです。

```bash
# 送信して message_id を取得
MID=$(curl -s -X POST "$WEBHOOK_URL?wait=true" \
  -H 'Content-Type: application/json' \
  -d '{"content":"処理中…"}' | jq -r .id)

# 編集(本文・embed を差し替え。通知は再送されません)
curl -X PATCH "$WEBHOOK_URL/messages/$MID" \
  -H 'Content-Type: application/json' \
  -d '{"content":"✅ 完了しました"}'

# 取得 / 削除
curl "$WEBHOOK_URL/messages/$MID"
curl -X DELETE "$WEBHOOK_URL/messages/$MID"
```

- `PATCH` … `content` / `embeds` と添付を全置換。既存画像を残すには応答の attachment `id` を `attachments` に含め、画像追加には `files[n]` を指定します。`attachments` を省略すると既存画像は削除されます。`username` / `avatar_url` は指定時のみ更新。`200` でメッセージを返却。
- `GET` … メッセージオブジェクトを返却。
- `DELETE` … `204 No Content`。
- 編集・削除は開いている画面に **SSE でその場反映**(編集は再通知なし、Discord と同じ)。
- 他の Webhook が送ったメッセージは編集・削除できません(`404 Unknown Message`)。

## 運用メモ

- **データ**: 受理した画像バイトを含め、すべて `./data/noticord.db`(SQLite, WAL)。バックアップはこのファイルを退避するだけです。画像分だけ履歴保持時の DB サイズは増えます。
- **履歴保持**: 既定で最新 1000 件(`internal/config` の `KeepMessages`)。
- **VAPID 鍵**: admin 初回起動時に生成し DB に保存。鍵を失うと既存購読は無効化されるため、`./data` は保持すること。
- **UI 言語**: `NOTICORD_LANG` で `en` / `ja` を選択(既定 `en`)。サーバー起動時に決まり、`/api/me` 経由で PWA に渡されます。
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

UI 文言は `web/static/app.js` 冒頭の `I18N`(`en` / `ja`)に集約。HTML 側は `data-i18n` / `data-i18n-ph` / `data-i18n-title` 属性で対象を指定し、起動時に差し替えます。
