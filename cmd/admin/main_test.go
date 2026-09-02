package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"noticord/internal/config"
	"noticord/internal/db"
	"noticord/internal/ingest"
)

func TestAttachmentAPIIsAuthenticatedAndNotOnIngestMux(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	_, attachments, err := db.AddMessageWithAttachments(d, db.Message{
		ChannelID: "channel", Username: "test", Embeds: "[]", Raw: "{}",
	}, []db.Attachment{{
		Filename: "pixel.png", ContentType: "image/png", Size: 8,
		Data: []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a},
	}})
	if err != nil || len(attachments) != 1 {
		t.Fatalf("add attachment: %v, %v", attachments, err)
	}

	s := &server{db: d, cfg: config.Admin{Password: "secret"}, sessions: map[string]bool{"session": true}}
	adminMux := http.NewServeMux()
	adminMux.Handle("GET /api/attachments/{id}", s.protect(s.getAttachment))
	path := "/api/attachments/" + strconv.FormatInt(attachments[0].ID, 10)

	unauth := httptest.NewRecorder()
	adminMux.ServeHTTP(unauth, httptest.NewRequest(http.MethodGet, path, nil))
	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated attachment: got %d", unauth.Code)
	}

	authReq := httptest.NewRequest(http.MethodGet, path, nil)
	authReq.AddCookie(&http.Cookie{Name: sessionCookie, Value: "session"})
	auth := httptest.NewRecorder()
	adminMux.ServeHTTP(auth, authReq)
	if auth.Code != http.StatusOK || auth.Header().Get("Content-Type") != "image/png" || auth.Header().Get("X-Content-Type-Options") != "nosniff" || !bytes.Equal(auth.Body.Bytes(), attachmentsBytes()) {
		t.Fatalf("attachment response: status=%d headers=%v body=%x", auth.Code, auth.Header(), auth.Body.Bytes())
	}

	ingestMux := http.NewServeMux()
	(&ingest.Handler{DB: d}).Routes(ingestMux)
	public := httptest.NewRecorder()
	ingestMux.ServeHTTP(public, httptest.NewRequest(http.MethodGet, path, nil))
	if public.Code != http.StatusNotFound {
		t.Fatalf("ingest unexpectedly exposes attachment: got %d", public.Code)
	}
}

func attachmentsBytes() []byte {
	return []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
}
