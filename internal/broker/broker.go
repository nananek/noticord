// Package broker はプロセス内の軽量な pub/sub。
// admin プロセスは TCP(画面/SSE)と UDS(Discord 互換受信)の2つの net/http
// サーバーを同一プロセスで動かしているため、受信側が Publish し SSE 側が
// Subscribe することで、追加のミドルウェア無しにリアルタイム配信できる。
package broker

import "sync"

// Event は SSE で配信する1イベント。Type は "message" など。
// Data は JSON シリアライズ可能な任意のペイロード(通常はメッセージ1件)。
type Event struct {
	Type      string `json:"type"`
	ChannelID string `json:"channel_id"`
	Data      any    `json:"data,omitempty"`
}

// Broker は購読者へイベントをファンアウトする。イベントは取りこぼし可
// (履歴は SQLite が持つ)なので、遅い購読者向けにバッファ付き・ノンブロッキング送信。
type Broker struct {
	mu   sync.Mutex
	subs map[chan Event]struct{}
}

func New() *Broker {
	return &Broker{subs: make(map[chan Event]struct{})}
}

// Subscribe は新しい購読チャネルと解除関数を返す。
func (b *Broker) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 16)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.subs, ch)
			b.mu.Unlock()
			close(ch)
		})
	}
	return ch, cancel
}

// Publish は全購読者へイベントを送る。バッファが詰まっている購読者は
// その回をスキップする(ブロックしない)。
func (b *Broker) Publish(ev Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- ev:
		default:
			// 遅い購読者は取りこぼす。クライアントは再取得でリカバリ可能。
		}
	}
}

// Count は現在の購読者数を返す(デバッグ用)。
func (b *Broker) Count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs)
}
