// Package push は Web Push (VAPID) 送信の薄いラッパ。
package push

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	webpush "github.com/SherClockHolmes/webpush-go"
	"noticord/internal/db"
)

// EndpointHost は購読 endpoint からホスト名だけを取り出す。
// 送信先が Apple(web.push.apple.com)か FCM(fcm.googleapis.com)かを
// 秘密のパス部分を晒さずにログ判別するために使う。
func EndpointHost(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return "?"
	}
	return u.Host
}

// Notification は Service Worker へ渡す JSON ペイロード。
type Notification struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url,omitempty"`
	Tag   string `json:"tag,omitempty"`
}

type Sender struct {
	v     db.VAPID
	debug bool
}

func New(v db.VAPID, debug bool) Sender { return Sender{v: v, debug: debug} }

// Send は単一購読へ送信し、HTTP ステータスコードを返す。
// 404/410 は購読失効を意味するため、呼び出し側で購読削除に使う。
func (s Sender) Send(sub db.Subscription, n Notification) (int, error) {
	payload, err := json.Marshal(n)
	if err != nil {
		return 0, err
	}

	// 重要: webpush-go は JWT の sub クレームに自動で "mailto:" を前置する
	// (vapid.go: `"sub": fmt.Sprintf("mailto:%s", subscriber)`)。
	// したがって Subscriber には mailto: を含まない素のアドレスを渡す。
	// "mailto:foo@bar" をそのまま渡すと sub が "mailto:mailto:foo@bar" となり、
	// 厳格な Apple Push は 403 を返す(Mozilla は緩いため通ってしまう)。
	subscriber := strings.TrimPrefix(strings.TrimSpace(s.v.Subject), "mailto:")

	opts := &webpush.Options{
		Subscriber:      subscriber,
		VAPIDPublicKey:  s.v.Public,
		VAPIDPrivateKey: s.v.Private,
		TTL:             86400,
		Urgency:         webpush.UrgencyHigh,
	}
	// デバッグ時は 4xx/5xx のプッシュサービス応答ボディ(Apple は具体的な
	// 拒否理由を返す)を傍受してログする。
	if s.debug {
		opts.HTTPClient = &loggingClient{inner: http.DefaultClient}
	}

	resp, err := webpush.SendNotification(payload, &webpush.Subscription{
		Endpoint: sub.Endpoint,
		Keys: webpush.Keys{
			P256dh: sub.P256dh,
			Auth:   sub.Auth,
		},
	}, opts)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

// loggingClient は 4xx/5xx 時のプッシュサービス応答(host・status・本文)を
// ログする。秘密情報(JWT / Authorization / 購読鍵)は意図的にログしない。
type loggingClient struct {
	inner *http.Client
}

func (c *loggingClient) Do(req *http.Request) (*http.Response, error) {
	resp, err := c.inner.Do(req)
	if err != nil {
		return resp, err
	}
	if resp.StatusCode >= 400 {
		// ボディを読み切ってログし、呼び出し側が再度読めるよう詰め直す。
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		// プッシュサービス応答は外部入力。strconv.Quote でクォート・エスケープして
		// 改行注入を防ぐ(ログインジェクション対策。CodeQL のサニタイザに合致)。
		log.Printf("[debug] push response host=%s status=%d body=%s",
			req.URL.Host, resp.StatusCode, strconv.Quote(strings.TrimSpace(string(body))))
		resp.Body = io.NopCloser(bytes.NewReader(body))
	}
	return resp, err
}
