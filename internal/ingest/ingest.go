// Package ingest は Discord 互換 Webhook の受信処理(トークン検証・履歴保存・
// Web Push 送信)を提供する。admin プロセスが UDS 経由でこのハンドラを公開し、
// 受け口の webhook プロセスはここへ素通しで転送するだけにする。
package ingest

import (
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"noticord/internal/db"
	"noticord/internal/discord"
	"noticord/internal/push"
)

const maxBody = 1 << 20 // 1MiB

// Handler は受信エンドポイントの実装。DB を所有する admin から生成される。
type Handler struct {
	DB           *sql.DB
	KeepMessages int
}

// Routes は受信用ルートを mux に登録する(UDS 側サーバー専用)。
func (h *Handler) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("POST /api/webhooks/{id}/{token}", h.receive)
	mux.HandleFunc("GET /api/webhooks/{id}/{token}", h.info)
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
		"id":    t.ID,
		"type":  1,
		"name":  firstNonEmpty(t.Name, "noticord"),
		"token": t.Token,
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

	title, body := payload.Notification(firstNonEmpty(t.Name, "noticord"))

	embedsJSON := "[]"
	if len(payload.Embeds) > 0 {
		if b, e := json.Marshal(payload.Embeds); e == nil {
			embedsJSON = string(b)
		}
	}

	msgID, err := db.AddMessage(h.DB, db.Message{
		TokenID:   t.ID,
		Username:  title,
		Content:   payload.Content,
		Embeds:    embedsJSON,
		Raw:       raw,
		CreatedAt: time.Now().Unix(),
	})
	if err != nil {
		log.Printf("save message: %v", err)
	}
	_ = db.TouchToken(h.DB, t.ID)
	if h.KeepMessages > 0 {
		_ = db.PruneMessages(h.DB, h.KeepMessages)
	}

	h.fanout(push.Notification{Title: title, Body: body, URL: "/", Tag: "noticord"})

	if r.URL.Query().Get("wait") == "true" {
		writeJSON(w, http.StatusOK, map[string]any{
			"id":         msgID,
			"type":       0,
			"content":    payload.Content,
			"timestamp":  time.Now().UTC().Format(time.RFC3339),
			"webhook_id": t.ID,
		})
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
	sender := push.New(v)
	for _, sub := range subs {
		status, err := sender.Send(sub, n)
		if err != nil {
			log.Printf("push send error endpoint=%.40s err=%v", sub.Endpoint, err)
			continue
		}
		if status == http.StatusNotFound || status == http.StatusGone {
			_ = db.DeleteSubscriptionByEndpoint(h.DB, sub.Endpoint)
			log.Printf("pruned expired subscription endpoint=%.40s", sub.Endpoint)
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
