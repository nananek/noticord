// command admin は noticord の中核プロセス。PWA 配信・管理 API(Tailscale netns 上の TCP)に
// 加え、Discord 互換の受信を UDS で listen する。DB・VAPID・Web Push 送信をすべて所有し、
// cloudflared は UDS 経由でこの受信口にだけ到達する。
package main

import (
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"noticord/internal/broker"
	"noticord/internal/config"
	"noticord/internal/db"
	"noticord/internal/ingest"
	"noticord/internal/push"
	"noticord/web"
)

func main() {
	cfg := config.LoadAdmin()

	d, err := db.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("db open: %v", err)
	}
	defer d.Close()

	vapid, err := db.EnsureVAPID(d, cfg.VAPIDSubject)
	if err != nil {
		log.Fatalf("ensure vapid: %v", err)
	}
	log.Printf("VAPID public key: %s", vapid.Public)

	bkr := broker.New()
	s := &server{db: d, cfg: cfg, sessions: map[string]bool{}, broker: bkr}

	static, err := fs.Sub(web.FS, "static")
	if err != nil {
		log.Fatalf("static fs: %v", err)
	}
	fileServer := http.FileServer(http.FS(static))

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })

	// 認証関連
	mux.HandleFunc("POST /api/login", s.login)
	mux.HandleFunc("POST /api/logout", s.logout)
	mux.HandleFunc("GET /api/me", s.me)

	// 保護対象 API
	mux.Handle("GET /api/vapid-public-key", s.protect(s.vapidKey))
	mux.Handle("POST /api/subscribe", s.protect(s.subscribe))
	mux.Handle("POST /api/unsubscribe", s.protect(s.unsubscribe))

	// チャンネル
	mux.Handle("GET /api/channels", s.protect(s.listChannels))
	mux.Handle("POST /api/channels", s.protect(s.createChannel))
	mux.Handle("PATCH /api/channels/{id}", s.protect(s.updateChannel))
	mux.Handle("DELETE /api/channels/{id}", s.protect(s.deleteChannel))
	mux.Handle("GET /api/channels/{id}/webhooks", s.protect(s.listChannelWebhooks))
	mux.Handle("POST /api/channels/{id}/webhooks", s.protect(s.createChannelWebhook))
	mux.Handle("GET /api/channels/{id}/messages", s.protect(s.listChannelMessages))
	mux.Handle("POST /api/channels/{id}/messages/clear", s.protect(s.clearChannelMessages))

	// Webhook / メッセージ単体操作
	mux.Handle("DELETE /api/tokens/{id}", s.protect(s.deleteToken))
	mux.Handle("DELETE /api/messages/{id}", s.protect(s.deleteMessage))
	mux.Handle("POST /api/test", s.protect(s.testPush))

	// 開いている画面へのリアルタイム配信(SSE)
	mux.Handle("GET /api/events", s.protect(s.events))

	// PWA 静的ファイル
	mux.Handle("GET /", fileServer)

	// Discord 互換の受信は UDS で listen する(cloudflared が直結する唯一の接点)。
	// ここには受信ルートしか載せないので、管理 API/履歴はソケットからは触れられない。
	startIngestServer(d, cfg, bkr)

	hs := &http.Server{
		Addr:              cfg.Listen,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("admin listening on %s (db=%s, auth=%v)", cfg.Listen, cfg.DBPath, cfg.Password != "")
	log.Fatal(hs.ListenAndServe())
}

// startIngestServer は受信専用ハンドラを UDS で公開する。
func startIngestServer(d *sql.DB, cfg config.Admin, bkr *broker.Broker) {
	if err := os.MkdirAll(filepath.Dir(cfg.IngestSocket), 0o770); err != nil {
		log.Fatalf("ingest socket dir: %v", err)
	}
	// 前回の残骸を掃除してから listen する。
	_ = os.Remove(cfg.IngestSocket)
	ln, err := net.Listen("unix", cfg.IngestSocket)
	if err != nil {
		log.Fatalf("ingest listen: %v", err)
	}
	// cloudflared(同一 uid 65532)が connect できるよう所有者+グループに rw を与える。
	if err := os.Chmod(cfg.IngestSocket, 0o660); err != nil {
		log.Printf("chmod socket: %v", err)
	}

	h := &ingest.Handler{DB: d, KeepMessages: cfg.KeepMessages, Debug: cfg.Debug, Broker: bkr}
	imux := http.NewServeMux()
	h.Routes(imux)
	isrv := &http.Server{Handler: imux, ReadHeaderTimeout: 10 * time.Second}

	go func() {
		log.Printf("ingest listening on unix:%s", cfg.IngestSocket)
		if err := isrv.Serve(ln); err != nil {
			log.Fatalf("ingest serve: %v", err)
		}
	}()
}

type server struct {
	db     *sql.DB
	cfg    config.Admin
	broker *broker.Broker

	mu       sync.Mutex
	sessions map[string]bool
}

const sessionCookie = "noticord_session"

// ---- 認証 ----

func (s *server) authEnabled() bool { return s.cfg.Password != "" }

func (s *server) isAuthed(r *http.Request) bool {
	if !s.authEnabled() {
		return true
	}
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessions[c.Value]
}

func (s *server) protect(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.isAuthed(r) {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
			return
		}
		h(w, r)
	})
}

func (s *server) login(w http.ResponseWriter, r *http.Request) {
	if !s.authEnabled() {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad request"})
		return
	}
	if subtle.ConstantTimeCompare([]byte(body.Password), []byte(s.cfg.Password)) != 1 {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid password"})
		return
	}
	tok := randToken()
	s.mu.Lock()
	s.sessions[tok] = true
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    tok,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   60 * 60 * 24 * 30,
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.mu.Lock()
		delete(s.sessions, c.Value)
		s.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *server) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"authed":        s.isAuthed(r),
		"auth_required": s.authEnabled(),
	})
}

// ---- Web Push 購読 ----

func (s *server) vapidKey(w http.ResponseWriter, r *http.Request) {
	v, err := db.LoadVAPID(s.db)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "vapid"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"key": v.Public})
}

func (s *server) subscribe(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Endpoint string `json:"endpoint"`
		Keys     struct {
			P256dh string `json:"p256dh"`
			Auth   string `json:"auth"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Endpoint == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad subscription"})
		return
	}
	if err := db.AddSubscription(s.db, body.Endpoint, body.Keys.P256dh, body.Keys.Auth); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *server) unsubscribe(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Endpoint string `json:"endpoint"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Endpoint == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad request"})
		return
	}
	if err := db.DeleteSubscriptionByEndpoint(s.db, body.Endpoint); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---- トークン管理 ----

type tokenView struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	URL        string `json:"url"`
	CreatedAt  int64  `json:"created_at"`
	LastUsedAt int64  `json:"last_used_at"`
}

func (s *server) toView(t db.Token) tokenView {
	base := s.cfg.PublicURL
	if base == "" {
		base = "<NOTICORD_PUBLIC_URL>"
	}
	return tokenView{
		ID:         t.ID,
		Name:       t.Name,
		URL:        base + "/api/webhooks/" + t.ID + "/" + t.Token,
		CreatedAt:  t.CreatedAt,
		LastUsedAt: t.LastUsedAt,
	}
}

// ---- チャンネル ----

func (s *server) listChannels(w http.ResponseWriter, r *http.Request) {
	cs, err := db.ListChannels(s.db)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	if cs == nil {
		cs = []db.Channel{}
	}
	writeJSON(w, http.StatusOK, cs)
}

func (s *server) createChannel(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name  string `json:"name"`
		Topic string `json:"topic"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	name := normalizeChannelName(body.Name)
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "チャンネル名を入力してください"})
		return
	}
	c, err := db.CreateChannel(s.db, name, strings.TrimSpace(body.Topic))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *server) updateChannel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ch, err := db.GetChannel(s.db, id)
	if err != nil || ch == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "channel not found"})
		return
	}
	var body struct {
		Name  string `json:"name"`
		Topic string `json:"topic"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	name := normalizeChannelName(body.Name)
	if name == "" {
		name = ch.Name
	}
	if err := db.UpdateChannel(s.db, id, name, strings.TrimSpace(body.Topic)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	c, _ := db.GetChannel(s.db, id)
	writeJSON(w, http.StatusOK, c)
}

func (s *server) deleteChannel(w http.ResponseWriter, r *http.Request) {
	// 常に最低1チャンネルは残す。
	n, err := db.CountChannels(s.db)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	if n <= 1 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "最後のチャンネルは削除できません"})
		return
	}
	if err := db.DeleteChannel(s.db, r.PathValue("id")); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// normalizeChannelName は Discord 風に小文字化し、空白を - に、許可外文字を除去する。
func normalizeChannelName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "#")
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

// ---- Webhook(チャンネル配下) ----

func (s *server) listChannelWebhooks(w http.ResponseWriter, r *http.Request) {
	ts, err := db.ListTokensByChannel(s.db, r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	out := make([]tokenView, 0, len(ts))
	for _, t := range ts {
		out = append(out, s.toView(t))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) createChannelWebhook(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ch, err := db.GetChannel(s.db, id)
	if err != nil || ch == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "channel not found"})
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	t, err := db.CreateToken(s.db, id, strings.TrimSpace(body.Name))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, s.toView(t))
}

func (s *server) deleteToken(w http.ResponseWriter, r *http.Request) {
	if err := db.DeleteToken(s.db, r.PathValue("id")); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---- 履歴 ----

func (s *server) listChannelMessages(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	ms, err := db.ListMessagesByChannel(s.db, r.PathValue("id"), limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	if ms == nil {
		ms = []db.Message{}
	}
	writeJSON(w, http.StatusOK, ms)
}

func (s *server) deleteMessage(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad id"})
		return
	}
	if err := db.DeleteMessage(s.db, id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *server) clearChannelMessages(w http.ResponseWriter, r *http.Request) {
	if err := db.ClearMessagesByChannel(s.db, r.PathValue("id")); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---- テスト送信 ----

func (s *server) testPush(w http.ResponseWriter, r *http.Request) {
	v, err := db.LoadVAPID(s.db)
	if err != nil || v.Public == "" {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "vapid not ready"})
		return
	}
	subs, err := db.ListSubscriptions(s.db)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	sender := push.New(v, s.cfg.Debug)
	sent := 0
	// デバイスごとの送信結果(ホスト+ステータス)を返し、UI で切り分けできるようにする。
	results := make([]map[string]any, 0, len(subs))
	for _, sub := range subs {
		host := push.EndpointHost(sub.Endpoint)
		status, err := sender.Send(sub, push.Notification{
			Title: "noticord",
			Body:  "テスト通知です 🔔",
			URL:   "/",
			Tag:   "noticord-test",
		})
		r := map[string]any{"host": host, "id": sub.ID}
		if err != nil {
			r["error"] = err.Error()
			if s.cfg.Debug {
				log.Printf("[debug] test push error host=%s id=%d err=%v", host, sub.ID, err)
			}
			results = append(results, r)
			continue
		}
		r["status"] = status
		if s.cfg.Debug {
			log.Printf("[debug] test push host=%s id=%d status=%d", host, sub.ID, status)
		}
		if status == http.StatusNotFound || status == http.StatusGone {
			_ = db.DeleteSubscriptionByEndpoint(s.db, sub.Endpoint)
			r["pruned"] = true
		} else if status < 400 {
			sent++
		}
		results = append(results, r)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"sent": sent, "subscriptions": len(subs), "results": results,
	})
}

// ---- SSE(リアルタイム配信) ----

// events は Server-Sent Events で受信メッセージを開いている画面へ流す。
func (s *server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // プロキシのバッファリング抑止

	ch, cancel := s.broker.Subscribe()
	defer cancel()

	// 接続確立を即通知(EventSource の onopen を発火させる)。
	w.Write([]byte("event: ready\ndata: {}\n\n"))
	flusher.Flush()

	ctx := r.Context()
	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case ev, alive := <-ch:
			if !alive {
				return
			}
			b, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			if _, err := w.Write([]byte("event: " + ev.Type + "\ndata: " + string(b) + "\n\n")); err != nil {
				return
			}
			flusher.Flush()
		case <-ticker.C:
			// アイドル接続維持用のコメント行(プロキシのタイムアウト回避)。
			if _, err := w.Write([]byte(": keepalive\n\n")); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// ---- ユーティリティ ----

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func randToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}
