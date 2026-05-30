package ingest

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"noticord/internal/db"
)

// setup は一時 DB・チャンネル・Webhook を用意し、テスト用 HTTP サーバーを返す。
func setup(t *testing.T) (*httptest.Server, db.Token) {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { d.Close() })

	ch, err := db.CreateChannel(d, "alerts", "")
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	tok, err := db.CreateToken(d, ch.ID, "grafana")
	if err != nil {
		t.Fatalf("create token: %v", err)
	}

	mux := http.NewServeMux()
	h := &Handler{DB: d}
	h.Routes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, tok
}

func do(t *testing.T, method, url, body string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, string(b)
}

// TestMessageEditLifecycle は Discord 互換の送信→編集→取得→削除の往復を検証する。
func TestMessageEditLifecycle(t *testing.T) {
	srv, tok := setup(t)
	base := srv.URL + "/api/webhooks/" + tok.ID + "/" + tok.Token

	// 1) wait=true で送信し message_id を得る。
	resp, body := do(t, http.MethodPost, base+"?wait=true", `{"content":"before"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST wait=true: got %d body=%s", resp.StatusCode, body)
	}
	mid := extract(t, body, `"id":"`)

	// 2) PATCH で編集。
	resp, body = do(t, http.MethodPatch, base+"/messages/"+mid, `{"content":"after"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH: got %d body=%s", resp.StatusCode, body)
	}
	if !strings.Contains(body, `"content":"after"`) {
		t.Fatalf("PATCH response not updated: %s", body)
	}

	// 3) GET で編集が永続化されたか確認。
	resp, body = do(t, http.MethodGet, base+"/messages/"+mid, "")
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, `"content":"after"`) {
		t.Fatalf("GET after edit: got %d body=%s", resp.StatusCode, body)
	}

	// 4) DELETE。
	resp, _ = do(t, http.MethodDelete, base+"/messages/"+mid, "")
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE: got %d", resp.StatusCode)
	}

	// 5) 削除後の GET は 404。
	resp, _ = do(t, http.MethodGet, base+"/messages/"+mid, "")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET after delete: got %d, want 404", resp.StatusCode)
	}
}

// TestEditWrongTokenRejected は別 Webhook のメッセージを編集できないことを確認する。
func TestEditWrongTokenRejected(t *testing.T) {
	srv, tok := setup(t)
	base := srv.URL + "/api/webhooks/" + tok.ID + "/" + tok.Token

	_, body := do(t, http.MethodPost, base+"?wait=true", `{"content":"x"}`)
	mid := extract(t, body, `"id":"`)

	// 不正トークンで PATCH → 401。
	bad := srv.URL + "/api/webhooks/" + tok.ID + "/wrongtoken/messages/" + mid
	resp, _ := do(t, http.MethodPatch, bad, `{"content":"hack"}`)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("PATCH wrong token: got %d, want 401", resp.StatusCode)
	}
}

// extract は body から marker 直後の値(次の `"` まで)を取り出す簡易ヘルパ。
func extract(t *testing.T, body, marker string) string {
	t.Helper()
	i := strings.Index(body, marker)
	if i < 0 {
		t.Fatalf("marker %q not found in %s", marker, body)
	}
	rest := body[i+len(marker):]
	j := strings.IndexByte(rest, '"')
	if j < 0 {
		t.Fatalf("unterminated value after %q in %s", marker, body)
	}
	return rest[:j]
}
