// Package push は Web Push (VAPID) 送信の薄いラッパ。
package push

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
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
	// デバッグ時は実際に飛ぶ HTTP リクエスト/レスポンスを傍受してログする。
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

// loggingClient は VAPID リクエストの JWT クレームと、4xx/5xx 時の
// プッシュサービス応答ボディ(Apple は具体的な拒否理由を返す)をログする。
type loggingClient struct {
	inner *http.Client
}

func (c *loggingClient) Do(req *http.Request) (*http.Response, error) {
	logVAPIDRequest(req)
	resp, err := c.inner.Do(req)
	if err != nil {
		return resp, err
	}
	if resp.StatusCode >= 400 {
		// ボディを読み切ってログし、呼び出し側が再度読めるよう詰め直す。
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		log.Printf("[debug] push response host=%s status=%d body=%q",
			req.URL.Host, resp.StatusCode, strings.TrimSpace(string(body)))
		resp.Body = io.NopCloser(bytes.NewReader(body))
	}
	return resp, err
}

// logVAPIDRequest は Authorization: "vapid t=<jwt>, k=<key>" を分解し、
// JWT の aud/sub/exp クレームと、各値の先頭を秘密を晒さない範囲でログする。
func logVAPIDRequest(req *http.Request) {
	auth := req.Header.Get("Authorization")
	t, k := parseVAPIDAuth(auth)
	aud, sub, exp := decodeJWTClaims(t)
	log.Printf("[debug] vapid host=%s aud=%q sub=%q exp=%d auth=%.16s… k=%.16s…",
		req.URL.Host, aud, sub, exp, auth, k)
}

// parseVAPIDAuth は "vapid t=<jwt>, k=<key>" から t と k を取り出す。
func parseVAPIDAuth(auth string) (t, k string) {
	auth = strings.TrimPrefix(auth, "vapid ")
	for _, part := range strings.Split(auth, ",") {
		part = strings.TrimSpace(part)
		if v, ok := strings.CutPrefix(part, "t="); ok {
			t = v
		} else if v, ok := strings.CutPrefix(part, "k="); ok {
			k = v
		}
	}
	return t, k
}

// decodeJWTClaims は JWT のペイロード(2番目のセグメント)から
// aud/sub/exp を取り出す。署名検証はしない(ログ用途)。
func decodeJWTClaims(jwt string) (aud, sub string, exp int64) {
	parts := strings.Split(jwt, ".")
	if len(parts) < 2 {
		return "", "", 0
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", "", 0
	}
	var c struct {
		Aud string `json:"aud"`
		Sub string `json:"sub"`
		Exp int64  `json:"exp"`
	}
	_ = json.Unmarshal(raw, &c)
	return c.Aud, c.Sub, c.Exp
}
