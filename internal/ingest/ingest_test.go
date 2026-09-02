package ingest

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"noticord/internal/db"
)

// setup は一時 DB・チャンネル・Webhook を用意し、テスト用 HTTP サーバーを返す。
func setup(t *testing.T) (*httptest.Server, db.Token) {
	t.Helper()
	srv, tok, _ := setupWithDB(t)
	return srv, tok
}

func setupWithDB(t *testing.T) (*httptest.Server, db.Token, *sql.DB) {
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
	return srv, tok, d
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

func doRequest(t *testing.T, req *http.Request) (*http.Response, []byte) {
	t.Helper()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", req.Method, req.URL, err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, b
}

// multipartImageRequest builds Discord's payload_json + files[n] form without
// relying on CreateFormFile's application/octet-stream default.
func multipartImageRequest(t *testing.T, method, url string, payload any, files []struct {
	Index, Name, ContentType string
	Data                     []byte
}) *http.Request {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.WriteField("payload_json", string(raw)); err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		h := make(textproto.MIMEHeader)
		h.Set("Content-Disposition", `form-data; name="files[`+f.Index+`]"; filename="`+f.Name+`"`)
		h.Set("Content-Type", f.ContentType)
		part, err := w.CreatePart(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(f.Data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(method, url, &body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}

// png is a valid 1x1 transparent PNG. It exercises both multipart metadata and
// magic-byte validation, not just the client-provided Content-Type.
var png = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
	0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
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

func TestMultipartImageLifecycle(t *testing.T) {
	srv, tok, d := setupWithDB(t)
	base := srv.URL + "/api/webhooks/" + tok.ID + "/" + tok.Token
	postPayload := map[string]any{
		"attachments": []map[string]any{{"id": 0, "filename": "pixel.png", "description": "first image"}},
		"embeds":      []map[string]any{{"image": map[string]any{"url": "attachment://pixel.png"}}},
	}
	resp, body := doRequest(t, multipartImageRequest(t, http.MethodPost, base+"?wait=true", postPayload, []struct {
		Index, Name, ContentType string
		Data                     []byte
	}{{"0", "pixel.png", "image/png", png}}))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("multipart POST: got %d body=%s", resp.StatusCode, body)
	}
	var posted struct {
		ID          string `json:"id"`
		Attachments []struct {
			ID          string `json:"id"`
			Filename    string `json:"filename"`
			Description string `json:"description"`
			ContentType string `json:"content_type"`
			Size        int64  `json:"size"`
		} `json:"attachments"`
	}
	if err := json.Unmarshal(body, &posted); err != nil {
		t.Fatal(err)
	}
	if len(posted.Attachments) != 1 || posted.Attachments[0].Filename != "pixel.png" || posted.Attachments[0].Description != "first image" || posted.Attachments[0].ContentType != "image/png" || posted.Attachments[0].Size != int64(len(png)) {
		t.Fatalf("unexpected attachment response: %s", body)
	}
	firstID, err := strconv.ParseInt(posted.Attachments[0].ID, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := db.GetAttachment(d, firstID)
	if err != nil || stored == nil || !bytes.Equal(stored.Data, png) {
		t.Fatalf("stored image = %#v, %v", stored, err)
	}

	// PATCH retains the returned image ID and appends files[0].
	patchPayload := map[string]any{
		"content": "with two images",
		"attachments": []map[string]any{
			{"id": posted.Attachments[0].ID, "filename": "pixel.png"},
			{"id": 0, "filename": "second.png", "description": "second image"},
		},
	}
	resp, body = doRequest(t, multipartImageRequest(t, http.MethodPatch, base+"/messages/"+posted.ID, patchPayload, []struct {
		Index, Name, ContentType string
		Data                     []byte
	}{{"0", "second.png", "image/png", png}}))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("multipart PATCH: got %d body=%s", resp.StatusCode, body)
	}
	var patched struct {
		Attachments []struct {
			ID string `json:"id"`
		} `json:"attachments"`
	}
	if err := json.Unmarshal(body, &patched); err != nil || len(patched.Attachments) != 2 {
		t.Fatalf("PATCH attachments=%s err=%v", body, err)
	}
	secondID, err := strconv.ParseInt(patched.Attachments[1].ID, 10, 64)
	if err != nil {
		t.Fatal(err)
	}

	// PATCH without attachments uses the documented replacement semantics.
	resp, bodyText := do(t, http.MethodPatch, base+"/messages/"+posted.ID, `{"content":"no images"}`)
	if resp.StatusCode != http.StatusOK || strings.Contains(bodyText, "pixel.png") {
		t.Fatalf("remove attachments PATCH: got %d body=%s", resp.StatusCode, bodyText)
	}
	for _, id := range []int64{firstID, secondID} {
		a, err := db.GetAttachment(d, id)
		if err != nil || a != nil {
			t.Fatalf("attachment %d remains after PATCH: %#v, %v", id, a, err)
		}
	}
}

func TestRejectsInvalidMultipartWithoutResidue(t *testing.T) {
	srv, tok, d := setupWithDB(t)
	base := srv.URL + "/api/webhooks/" + tok.ID + "/" + tok.Token
	payload := map[string]any{"attachments": []map[string]any{{"id": 0, "filename": "not-image.png"}}}
	resp, body := doRequest(t, multipartImageRequest(t, http.MethodPost, base, payload, []struct {
		Index, Name, ContentType string
		Data                     []byte
	}{{"0", "not-image.png", "image/png", []byte("not a png")}}))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad image: got %d body=%s", resp.StatusCode, body)
	}
	var count int
	if err := d.QueryRow("SELECT COUNT(*) FROM messages").Scan(&count); err != nil || count != 0 {
		t.Fatalf("invalid multipart left messages=%d err=%v", count, err)
	}
	if err := d.QueryRow("SELECT COUNT(*) FROM attachments").Scan(&count); err != nil || count != 0 {
		t.Fatalf("invalid multipart left attachments=%d err=%v", count, err)
	}
}

func TestAttachmentDeleteCascades(t *testing.T) {
	_, _, d := setupWithDB(t)
	mid, attachments, err := db.AddMessageWithAttachments(d, db.Message{ChannelID: "channel", Username: "test", Embeds: "[]", Raw: "{}"}, []db.Attachment{{
		Filename: "pixel.png", ContentType: "image/png", Size: int64(len(png)), Data: png,
	}})
	if err != nil || len(attachments) != 1 {
		t.Fatalf("add message attachment: id=%d attachments=%v err=%v", mid, attachments, err)
	}
	if err := db.DeleteMessage(d, mid); err != nil {
		t.Fatal(err)
	}
	a, err := db.GetAttachment(d, attachments[0].ID)
	if err != nil || a != nil {
		t.Fatalf("cascade failed: %#v, %v", a, err)
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
