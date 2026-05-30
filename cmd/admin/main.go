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

	s := &server{db: d, cfg: cfg, sessions: map[string]bool{}}

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
	mux.Handle("GET /api/tokens", s.protect(s.listTokens))
	mux.Handle("POST /api/tokens", s.protect(s.createToken))
	mux.Handle("DELETE /api/tokens/{id}", s.protect(s.deleteToken))
	mux.Handle("GET /api/messages", s.protect(s.listMessages))
	mux.Handle("DELETE /api/messages/{id}", s.protect(s.deleteMessage))
	mux.Handle("POST /api/messages/clear", s.protect(s.clearMessages))
	mux.Handle("POST /api/test", s.protect(s.testPush))

	// PWA 静的ファイル
	mux.Handle("GET /", fileServer)

	// Discord 互換の受信は UDS で listen する(cloudflared が直結する唯一の接点)。
	// ここには受信ルートしか載せないので、管理 API/履歴はソケットからは触れられない。
	startIngestServer(d, cfg)

	hs := &http.Server{
		Addr:              cfg.Listen,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("admin listening on %s (db=%s, auth=%v)", cfg.Listen, cfg.DBPath, cfg.Password != "")
	log.Fatal(hs.ListenAndServe())
}

// startIngestServer は受信専用ハンドラを UDS で公開する。
func startIngestServer(d *sql.DB, cfg config.Admin) {
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

	h := &ingest.Handler{DB: d, KeepMessages: cfg.KeepMessages}
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
	db  *sql.DB
	cfg config.Admin

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

func (s *server) listTokens(w http.ResponseWriter, r *http.Request) {
	ts, err := db.ListTokens(s.db)
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

func (s *server) createToken(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	t, err := db.CreateToken(s.db, strings.TrimSpace(body.Name))
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

func (s *server) listMessages(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	ms, err := db.ListMessages(s.db, limit)
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

func (s *server) clearMessages(w http.ResponseWriter, r *http.Request) {
	if err := db.ClearMessages(s.db); err != nil {
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
	sender := push.New(v)
	sent := 0
	for _, sub := range subs {
		status, err := sender.Send(sub, push.Notification{
			Title: "noticord",
			Body:  "テスト通知です 🔔",
			URL:   "/",
			Tag:   "noticord-test",
		})
		if err != nil {
			continue
		}
		if status == http.StatusNotFound || status == http.StatusGone {
			_ = db.DeleteSubscriptionByEndpoint(s.db, sub.Endpoint)
			continue
		}
		sent++
	}
	writeJSON(w, http.StatusOK, map[string]any{"sent": sent, "subscriptions": len(subs)})
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
