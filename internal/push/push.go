// Package push は Web Push (VAPID) 送信の薄いラッパ。
package push

import (
	"encoding/json"
	"net/url"

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
	v db.VAPID
}

func New(v db.VAPID) Sender { return Sender{v: v} }

// Send は単一購読へ送信し、HTTP ステータスコードを返す。
// 404/410 は購読失効を意味するため、呼び出し側で購読削除に使う。
func (s Sender) Send(sub db.Subscription, n Notification) (int, error) {
	payload, err := json.Marshal(n)
	if err != nil {
		return 0, err
	}
	resp, err := webpush.SendNotification(payload, &webpush.Subscription{
		Endpoint: sub.Endpoint,
		Keys: webpush.Keys{
			P256dh: sub.P256dh,
			Auth:   sub.Auth,
		},
	}, &webpush.Options{
		Subscriber:      s.v.Subject,
		VAPIDPublicKey:  s.v.Public,
		VAPIDPrivateKey: s.v.Private,
		TTL:             86400,
		Urgency:         webpush.UrgencyHigh,
	})
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}
