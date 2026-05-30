// Package ingest は Discord 互換 Webhook の受信処理(トークン検証・履歴保存・
// Web Push 送信)を提供する。admin プロセスが UDS でこのハンドラを公開し、
// cloudflared はここへ素通しで到達する。
package ingest

import (
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"noticord/internal/broker"
	"noticord/internal/db"
	"noticord/internal/discord"
	"noticord/internal/push"
)

const maxBody = 1 << 20 // 1MiB

// Handler は受信エンドポイントの実装。DB を所有する admin から生成される。
type Handler struct {
	DB           *sql.DB
	KeepMessages int
	Debug        bool           // 受信内容と各購読への送信結果を詳細ログする
	Broker       *broker.Broker // 受信メッセージを SSE 購読者へリアルタイム配信する
}

func (h *Handler) debugf(format string, args ...any) {
	if !h.Debug {
		return
	}
	// 外部由来の文字列(チャンネル名・Webhook名・通知タイトル等)が
	// ログに改行・制御文字を注入する(ログインジェクション)のを防ぐため、
	// 文字列引数を strconv.Quote でクォート・エスケープしてからフォーマットする。
	// 呼び出し側の対応する書式指定子は %s にしておくこと(値はクォート済み)。
	clean := make([]any, len(args))
	for i, a := range args {
		if s, ok := a.(string); ok {
			clean[i] = strconv.Quote(s)
		} else {
			clean[i] = a
		}
	}
	log.Printf("[debug] "+format, clean...)
}

// Routes は受信用ルートを mux に登録する(UDS 側サーバー専用)。
func (h *Handler) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("POST /api/webhooks/{id}/{token}", h.receive)
	mux.HandleFunc("GET /api/webhooks/{id}/{token}", h.info)
	// Discord 互換: wait=true で得た message_id を後から編集/削除/取得する。
	mux.HandleFunc("GET /api/webhooks/{id}/{token}/messages/{mid}", h.getMessage)
	mux.HandleFunc("PATCH /api/webhooks/{id}/{token}/messages/{mid}", h.editMessage)
	mux.HandleFunc("DELETE /api/webhooks/{id}/{token}/messages/{mid}", h.deleteMessage)
}

// resolveMessage は token 認証のうえ、対象メッセージが「その Webhook 発」かを検証して返す。
// 認証失敗は 401、メッセージ不在/別 Webhook のものは 404(Discord の Unknown Message 相当)。
func (h *Handler) resolveMessage(w http.ResponseWriter, r *http.Request) (*db.Token, *db.Message) {
	t := h.auth(r.PathValue("id"), r.PathValue("token"))
	if t == nil {
		http.Error(w, `{"message":"Unknown Webhook","code":10015}`, http.StatusUnauthorized)
		return nil, nil
	}
	mid, err := strconv.ParseInt(r.PathValue("mid"), 10, 64)
	if err != nil {
		http.Error(w, `{"message":"Unknown Message","code":10008}`, http.StatusNotFound)
		return nil, nil
	}
	m, err := db.GetMessage(h.DB, mid)
	if err != nil {
		http.Error(w, `{"message":"Internal Server Error"}`, http.StatusInternalServerError)
		return nil, nil
	}
	// 別の Webhook が発したメッセージは触らせない(改竄防止)。
	if m == nil || m.TokenID != t.ID {
		http.Error(w, `{"message":"Unknown Message","code":10008}`, http.StatusNotFound)
		return nil, nil
	}
	return t, m
}

// messageObject は GET/PATCH 応答用の Discord 風メッセージオブジェクトを組み立てる。
func messageObject(m *db.Message) map[string]any {
	return map[string]any{
		"id":         strconv.FormatInt(m.ID, 10),
		"type":       0,
		"channel_id": m.ChannelID,
		"webhook_id": m.TokenID,
		"content":    m.Content,
		"embeds":     json.RawMessage(firstNonEmpty(m.Embeds, "[]")),
		"timestamp":  time.Unix(m.CreatedAt, 0).UTC().Format(time.RFC3339),
	}
}

// getMessage は Webhook が過去に送ったメッセージ1件を返す(Discord 互換 GET)。
func (h *Handler) getMessage(w http.ResponseWriter, r *http.Request) {
	_, m := h.resolveMessage(w, r)
	if m == nil {
		return
	}
	writeJSON(w, http.StatusOK, messageObject(m))
}

// editMessage はメッセージ本文/embed を上書きする(Discord 互換 PATCH)。
// 編集では Web Push を再送しない(Discord 同様、編集は通知を出さない)。
func (h *Handler) editMessage(w http.ResponseWriter, r *http.Request) {
	t, m := h.resolveMessage(w, r)
	if m == nil {
		return
	}

	payload, raw, err := parsePayload(r)
	if err != nil {
		http.Error(w, `{"message":"Cannot send an empty message","code":50006}`, http.StatusBadRequest)
		return
	}

	// 本文・embed は全置換。表示名/avatar は指定があれば差し替え、無ければ元を保持。
	username, _ := payload.Notification(firstNonEmpty(t.Name, "noticord"))
	if strings.TrimSpace(payload.Username) == "" {
		username = m.Username
	}
	avatar := m.Avatar
	if a := strings.TrimSpace(payload.AvatarURL); a != "" {
		avatar = a
	}
	embedsJSON := "[]"
	if len(payload.Embeds) > 0 {
		if b, e := json.Marshal(payload.Embeds); e == nil {
			embedsJSON = string(b)
		}
	}

	m.Username = username
	m.Avatar = avatar
	m.Content = payload.Content
	m.Embeds = embedsJSON
	m.Raw = raw
	if err := db.UpdateMessage(h.DB, *m); err != nil {
		log.Printf("update message: %v", err)
		http.Error(w, `{"message":"Internal Server Error"}`, http.StatusInternalServerError)
		return
	}

	// 開いている画面へ「その場編集」を配信する。
	if h.Broker != nil {
		h.Broker.Publish(broker.Event{Type: "update", ChannelID: m.ChannelID, Data: m})
	}
	h.debugf("edit channel=%s webhook=%s(%s) mid=%d content_len=%d embeds=%d",
		m.ChannelID, t.Name, t.ID, m.ID, len(payload.Content), len(payload.Embeds))

	writeJSON(w, http.StatusOK, messageObject(m))
}

// deleteMessage はメッセージを削除する(Discord 互換 DELETE)。
func (h *Handler) deleteMessage(w http.ResponseWriter, r *http.Request) {
	t, m := h.resolveMessage(w, r)
	if m == nil {
		return
	}
	if err := db.DeleteMessage(h.DB, m.ID); err != nil {
		log.Printf("delete message: %v", err)
		http.Error(w, `{"message":"Internal Server Error"}`, http.StatusInternalServerError)
		return
	}
	if h.Broker != nil {
		h.Broker.Publish(broker.Event{Type: "delete", ChannelID: m.ChannelID, Data: map[string]any{"id": strconv.FormatInt(m.ID, 10)}})
	}
	h.debugf("delete channel=%s webhook=%s(%s) mid=%d", m.ChannelID, t.Name, t.ID, m.ID)
	w.WriteHeader(http.StatusNoContent)
}

// auth は id/token を検証し、対応する Token を返す。失敗時は nil。
func (h *Handler) auth(id, token string) *db.Token {
	t, err := db.GetToken(h.DB, id)
	if err != nil || t == nil {
		return nil
	}
	if subtle.ConstantTimeCompare([]byte(t.Token), []byte(token)) != 1 {
		return nil
	}
	return t
}

// info は Discord 風の Webhook 情報 GET 応答(疎通確認用)。
func (h *Handler) info(w http.ResponseWriter, r *http.Request) {
	t := h.auth(r.PathValue("id"), r.PathValue("token"))
	if t == nil {
		http.Error(w, `{"message":"Unknown Webhook","code":10015}`, http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":         t.ID,
		"type":       1,
		"name":       firstNonEmpty(t.Name, "noticord"),
		"channel_id": t.ChannelID,
		"token":      t.Token,
	})
}

func (h *Handler) receive(w http.ResponseWriter, r *http.Request) {
	t := h.auth(r.PathValue("id"), r.PathValue("token"))
	if t == nil {
		http.Error(w, `{"message":"Unknown Webhook","code":10015}`, http.StatusUnauthorized)
		return
	}

	payload, raw, err := parsePayload(r)
	if err != nil {
		http.Error(w, `{"message":"Cannot send an empty message","code":50006}`, http.StatusBadRequest)
		return
	}

	// 送信者名(embed の要約も含む)と本文を組み立てる。
	username, body := payload.Notification(firstNonEmpty(t.Name, "noticord"))

	// チャンネル名を解決し、通知タイトルに前置して「どのチャンネルか」を示す。
	chName := ""
	if ch, _ := db.GetChannel(h.DB, t.ChannelID); ch != nil {
		chName = ch.Name
	}
	notifTitle := username
	if chName != "" {
		notifTitle = "#" + chName + " · " + username
	}

	embedsJSON := "[]"
	if len(payload.Embeds) > 0 {
		if b, e := json.Marshal(payload.Embeds); e == nil {
			embedsJSON = string(b)
		}
	}

	msg := db.Message{
		TokenID:   t.ID,
		ChannelID: t.ChannelID,
		Username:  username,
		Avatar:    strings.TrimSpace(payload.AvatarURL),
		Content:   payload.Content,
		Embeds:    embedsJSON,
		Raw:       raw,
		CreatedAt: time.Now().Unix(),
	}
	msgID, err := db.AddMessage(h.DB, msg)
	if err != nil {
		log.Printf("save message: %v", err)
	}
	msg.ID = msgID
	_ = db.TouchToken(h.DB, t.ID)
	if h.KeepMessages > 0 {
		_ = db.PruneMessages(h.DB, h.KeepMessages)
	}

	// 開いている画面へリアルタイム配信する(SSE)。履歴は DB が持つので失敗は許容。
	if h.Broker != nil {
		h.Broker.Publish(broker.Event{Type: "message", ChannelID: t.ChannelID, Data: msg})
	}

	h.debugf("receive channel=#%s webhook=%s(%s) title=%s body_len=%d embeds=%d",
		chName, t.Name, t.ID, notifTitle, len(body), len(payload.Embeds))

	// 通知クリックで該当チャンネルを開けるよう URL にチャンネル ID を載せる。
	h.fanout(push.Notification{
		Title: notifTitle,
		Body:  body,
		URL:   "/?c=" + t.ChannelID,
		Tag:   "noticord-" + t.ChannelID,
	})

	if r.URL.Query().Get("wait") == "true" {
		// Discord 同様、message_id を文字列で含むメッセージオブジェクトを返す。
		// クライアントはこの id を使って後から PATCH/DELETE できる。
		writeJSON(w, http.StatusOK, messageObject(&msg))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// fanout は全購読へ Web Push を送り、失効した購読(404/410)を削除する。
func (h *Handler) fanout(n push.Notification) {
	v, err := db.LoadVAPID(h.DB)
	if err != nil || v.Public == "" || v.Private == "" {
		log.Printf("vapid not ready, skip push: %v", err)
		return
	}
	subs, err := db.ListSubscriptions(h.DB)
	if err != nil {
		log.Printf("list subscriptions: %v", err)
		return
	}
	h.debugf("fanout to %d subscription(s)", len(subs))
	sender := push.New(v, h.Debug)
	for _, sub := range subs {
		host := push.EndpointHost(sub.Endpoint)
		status, err := sender.Send(sub, n)
		if err != nil {
			// エラーは常時出す。送信先ホスト(apple/fcm 等)を添える。
			log.Printf("push send error host=%s id=%d err=%v", host, sub.ID, err)
			continue
		}
		// 送信先ホストとプッシュサービスの返却ステータスを記録する。
		// 2xx 以外(特に 4xx)が iPhone だけ届かない原因の手がかりになる。
		h.debugf("push sent host=%s id=%d status=%d", host, sub.ID, status)
		if status == http.StatusNotFound || status == http.StatusGone {
			_ = db.DeleteSubscriptionByEndpoint(h.DB, sub.Endpoint)
			log.Printf("pruned expired subscription host=%s id=%d (status=%d)", host, sub.ID, status)
		} else if status >= 400 {
			// 失効以外の拒否(例: 4xx/5xx)は常時警告する。
			log.Printf("push rejected host=%s id=%d status=%d", host, sub.ID, status)
		}
	}
}

// parsePayload は JSON / form(payload_json) / multipart(payload_json) を受ける。
func parsePayload(r *http.Request) (discord.Payload, string, error) {
	r.Body = io.NopCloser(io.LimitReader(r.Body, maxBody))
	ct := r.Header.Get("Content-Type")
	var raw string

	switch {
	case strings.HasPrefix(ct, "application/x-www-form-urlencoded"):
		if err := r.ParseForm(); err != nil {
			return discord.Payload{}, "", err
		}
		raw = r.PostFormValue("payload_json")
	case strings.HasPrefix(ct, "multipart/form-data"):
		if err := r.ParseMultipartForm(maxBody); err != nil {
			return discord.Payload{}, "", err
		}
		raw = r.FormValue("payload_json")
	default:
		buf, err := io.ReadAll(r.Body)
		if err != nil {
			return discord.Payload{}, "", err
		}
		raw = strings.TrimSpace(string(buf))
	}

	if raw == "" {
		return discord.Payload{}, "", errEmpty
	}
	var p discord.Payload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return discord.Payload{}, "", err
	}
	if strings.TrimSpace(p.Content) == "" && len(p.Embeds) == 0 {
		return discord.Payload{}, "", errEmpty
	}
	return p, raw, nil
}

var errEmpty = &emptyError{}

type emptyError struct{}

func (e *emptyError) Error() string { return "empty payload" }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
