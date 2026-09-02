// Package ingest は Discord 互換 Webhook の受信処理(トークン検証・履歴保存・
// Web Push 送信)を提供する。admin プロセスが UDS でこのハンドラを公開し、
// cloudflared はここへ素通しで到達する。
package ingest

import (
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"noticord/internal/broker"
	"noticord/internal/db"
	"noticord/internal/discord"
	"noticord/internal/push"
)

// maxBody is the maximum complete request size. Discord's default upload limit is
// 10 MiB; keeping the limit on the entire multipart body bounds both image bytes
// and attacker-controlled MIME metadata.
const maxBody int64 = 10 << 20 // 10 MiB

type uploadedImage struct {
	Index       int
	Filename    string
	Description string
	ContentType string
	Data        []byte
}

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
	attachments := make([]map[string]any, 0, len(m.Attachments))
	for _, a := range m.Attachments {
		// Webhook の公開 origin には画像 GET を置かない。ここで URL を返すと
		// CDN のように見えてしまうため、Discord 互換のメタデータだけを返す。
		attachments = append(attachments, map[string]any{
			"id":           strconv.FormatInt(a.ID, 10),
			"filename":     a.Filename,
			"description":  a.Description,
			"content_type": a.ContentType,
			"size":         a.Size,
		})
	}
	return map[string]any{
		"id":          strconv.FormatInt(m.ID, 10),
		"type":        0,
		"channel_id":  m.ChannelID,
		"webhook_id":  m.TokenID,
		"content":     m.Content,
		"embeds":      json.RawMessage(firstNonEmpty(m.Embeds, "[]")),
		"attachments": attachments,
		"timestamp":   time.Unix(m.CreatedAt, 0).UTC().Format(time.RFC3339),
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

	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	payload, raw, uploads, err := parsePayload(r)
	if err != nil {
		writePayloadError(w, err)
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
	keepIDs, err := retainedAttachmentIDs(m.Attachments, payload.Attachments, uploads)
	if err != nil {
		writePayloadError(w, err)
		return
	}
	attachments, err := db.UpdateMessageWithAttachments(h.DB, *m, keepIDs, dbAttachments(uploads))
	if err != nil {
		log.Printf("update message: %v", err)
		http.Error(w, `{"message":"Internal Server Error"}`, http.StatusInternalServerError)
		return
	}
	m.Attachments = attachments

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

	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	payload, raw, uploads, err := parsePayload(r)
	if err != nil {
		writePayloadError(w, err)
		return
	}
	if err := requireAllUploadsDescribed(payload.Attachments, uploads); err != nil {
		writePayloadError(w, err)
		return
	}

	// 送信者名(embed の要約も含む)と本文を組み立てる。
	username, body := payload.Notification(firstNonEmpty(t.Name, "noticord"))
	if strings.TrimSpace(payload.Content) == "" && len(uploads) > 0 {
		body = attachmentSummary(uploads)
	}

	// チャンネル名を解決し、通知タイトルに前置して「どのチャンネルか」を示す。
	// あわせてチャンネル別の通知設定(notify)を取得しておく。
	chName := ""
	chNotify := true
	if ch, _ := db.GetChannel(h.DB, t.ChannelID); ch != nil {
		chName = ch.Name
		chNotify = ch.Notify
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
	msgID, attachments, err := db.AddMessageWithAttachments(h.DB, msg, dbAttachments(uploads))
	if err != nil {
		log.Printf("save message: %v", err)
		http.Error(w, `{"message":"Internal Server Error"}`, http.StatusInternalServerError)
		return
	}
	msg.ID = msgID
	msg.Attachments = attachments
	_ = db.TouchToken(h.DB, t.ID)
	if h.KeepMessages > 0 {
		_ = db.PruneMessages(h.DB, h.KeepMessages)
	}

	// 開いている画面へリアルタイム配信する(SSE)。履歴は DB が持つので失敗は許容。
	if h.Broker != nil {
		h.Broker.Publish(broker.Event{Type: "message", ChannelID: t.ChannelID, Data: msg})
	}

	h.debugf("receive channel=#%s webhook=%s(%s) title=%s body_len=%d embeds=%d attachments=%d",
		chName, t.Name, t.ID, notifTitle, len(body), len(payload.Embeds), len(uploads))

	// チャンネル別通知設定: notify=OFF(ミュート)のチャンネルは Web Push を送らない。
	// 履歴保存・SSE 配信は済んでいるので、開いている画面には届く。
	if chNotify {
		// 通知クリックで該当チャンネルを開けるよう URL にチャンネル ID を載せる。
		h.fanout(push.Notification{
			Title: notifTitle,
			Body:  body,
			URL:   "/?c=" + t.ChannelID,
			Tag:   "noticord-" + t.ChannelID,
		})
	} else {
		h.debugf("channel #%s is muted, skip push", chName)
	}

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
// multipart の file handle と temporary file はこの関数を抜ける前に必ず閉じて削除し、
// 保存すべき画像だけを検証済みの byte slice として返す。呼び出し側はあらかじめ
// http.MaxBytesReader で r.Body を maxBody に包む。
func parsePayload(r *http.Request) (discord.Payload, string, []uploadedImage, error) {
	ct := r.Header.Get("Content-Type")
	var raw string
	var uploads []uploadedImage

	switch {
	case strings.HasPrefix(ct, "application/x-www-form-urlencoded"):
		if err := r.ParseForm(); err != nil {
			return discord.Payload{}, "", nil, err
		}
		raw = r.PostFormValue("payload_json")
	case strings.HasPrefix(ct, "multipart/form-data"):
		// Keep only a small amount in memory. Larger, still valid uploads are backed
		// by temporary files which RemoveAll below removes on every path.
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			return discord.Payload{}, "", nil, err
		}
		if r.MultipartForm == nil {
			return discord.Payload{}, "", nil, errInvalidAttachment
		}
		defer r.MultipartForm.RemoveAll()
		raw = r.FormValue("payload_json")
		var err error
		uploads, err = readUploadedImages(r)
		if err != nil {
			return discord.Payload{}, "", nil, err
		}
	default:
		buf, err := io.ReadAll(r.Body)
		if err != nil {
			return discord.Payload{}, "", nil, err
		}
		raw = strings.TrimSpace(string(buf))
	}

	if raw == "" {
		return discord.Payload{}, "", nil, errEmpty
	}
	var p discord.Payload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return discord.Payload{}, "", nil, err
	}
	if err := bindUploadMetadata(&p, uploads); err != nil {
		return discord.Payload{}, "", nil, err
	}
	if strings.TrimSpace(p.Content) == "" && len(p.Embeds) == 0 && len(p.Attachments) == 0 && len(uploads) == 0 {
		return discord.Payload{}, "", nil, errEmpty
	}
	return p, raw, uploads, nil
}

// readUploadedImages accepts only Discord's canonical files[n] parts. Validation
// is deliberately done on both declared MIME type and file signature: the stored
// type is later sent to the browser, so neither client-provided value is trusted.
func readUploadedImages(r *http.Request) ([]uploadedImage, error) {
	files := r.MultipartForm.File
	indices := make([]int, 0, len(files))
	byIndex := make(map[int]string, len(files))
	for field, headers := range files {
		idx, ok := filePartIndex(field)
		if !ok || len(headers) != 1 {
			return nil, fmt.Errorf("%w: expected one files[n] part", errInvalidAttachment)
		}
		if _, exists := byIndex[idx]; exists {
			return nil, fmt.Errorf("%w: duplicate file index", errInvalidAttachment)
		}
		byIndex[idx] = field
		indices = append(indices, idx)
	}
	sort.Ints(indices)
	out := make([]uploadedImage, 0, len(indices))
	for _, idx := range indices {
		h := files[byIndex[idx]][0]
		filename := h.Filename
		if !validFilename(filename) {
			return nil, fmt.Errorf("%w: invalid filename", errInvalidAttachment)
		}
		declared, _, err := mime.ParseMediaType(h.Header.Get("Content-Type"))
		if err != nil || !allowedImageType(strings.ToLower(declared)) {
			return nil, fmt.Errorf("%w: unsupported content type", errInvalidAttachment)
		}
		f, err := h.Open()
		if err != nil {
			return nil, err
		}
		data, readErr := func() ([]byte, error) {
			defer f.Close()
			return io.ReadAll(io.LimitReader(f, maxBody+1))
		}()
		if readErr != nil {
			return nil, readErr
		}
		if int64(len(data)) > maxBody || len(data) == 0 {
			return nil, fmt.Errorf("%w: image is empty or too large", errInvalidAttachment)
		}
		detected := sniffImageType(data)
		if detected == "" || detected != strings.ToLower(declared) {
			return nil, fmt.Errorf("%w: image signature does not match content type", errInvalidAttachment)
		}
		out = append(out, uploadedImage{Index: idx, Filename: filename, ContentType: detected, Data: data})
	}
	return out, nil
}

func filePartIndex(field string) (int, bool) {
	if !strings.HasPrefix(field, "files[") || !strings.HasSuffix(field, "]") {
		return 0, false
	}
	v := strings.TrimSuffix(strings.TrimPrefix(field, "files["), "]")
	idx, err := strconv.Atoi(v)
	if err != nil || idx < 0 || strconv.Itoa(idx) != v {
		return 0, false
	}
	return idx, true
}

func validFilename(name string) bool {
	if name == "" || len(name) > 255 || strings.TrimSpace(name) != name || strings.ContainsAny(name, "/\\") {
		return false
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func allowedImageType(contentType string) bool {
	switch contentType {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
		return true
	default:
		return false
	}
}

func sniffImageType(data []byte) string {
	switch {
	case len(data) >= 8 && string(data[:8]) == "\x89PNG\r\n\x1a\n":
		return "image/png"
	case len(data) >= 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff:
		return "image/jpeg"
	case len(data) >= 6 && (string(data[:6]) == "GIF87a" || string(data[:6]) == "GIF89a"):
		return "image/gif"
	case len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return "image/webp"
	default:
		return ""
	}
}

// bindUploadMetadata attaches an attachments[id=n] description to files[n].
// References without a matching upload are allowed here because PATCH uses them
// to retain an existing attachment; receive rejects them after parsing.
func bindUploadMetadata(p *discord.Payload, uploads []uploadedImage) error {
	refs := make(map[int]discord.Attachment, len(p.Attachments))
	for _, a := range p.Attachments {
		idx, err := attachmentIndex(a.ID)
		if err != nil || idx < 0 {
			return fmt.Errorf("%w: attachment id", errInvalidAttachment)
		}
		if _, exists := refs[idx]; exists {
			return fmt.Errorf("%w: duplicate attachment id", errInvalidAttachment)
		}
		if a.Filename != "" && !validFilename(a.Filename) {
			return fmt.Errorf("%w: invalid attachment filename", errInvalidAttachment)
		}
		refs[idx] = a
	}
	if p.Attachments == nil {
		return nil // Discord allows multipart files without the optional metadata array.
	}
	for i := range uploads {
		a, ok := refs[uploads[i].Index]
		if !ok {
			return fmt.Errorf("%w: missing metadata for files[%d]", errInvalidAttachment, uploads[i].Index)
		}
		if a.Filename != "" && a.Filename != uploads[i].Filename {
			return fmt.Errorf("%w: filename does not match files[%d]", errInvalidAttachment, uploads[i].Index)
		}
		uploads[i].Description = a.Description
	}
	return nil
}

func attachmentIndex(id discord.AttachmentID) (int, error) {
	v := string(id)
	if strings.TrimSpace(v) != v || v == "" {
		return 0, errors.New("empty attachment id")
	}
	i, err := strconv.ParseInt(v, 10, 64)
	if err != nil || i < 0 || i > int64(^uint(0)>>1) {
		return 0, errors.New("invalid attachment id")
	}
	return int(i), nil
}

// requireAllUploadsDescribed applies POST semantics: attachment references may
// only describe the submitted files, never a non-existent prior attachment.
func requireAllUploadsDescribed(refs []discord.Attachment, uploads []uploadedImage) error {
	if refs == nil {
		return nil
	}
	matched := make(map[int]bool, len(uploads))
	for _, u := range uploads {
		matched[u.Index] = true
	}
	for _, a := range refs {
		idx, err := attachmentIndex(a.ID)
		if err != nil || !matched[idx] {
			return fmt.Errorf("%w: attachment has no file", errInvalidAttachment)
		}
	}
	return nil
}

// retainedAttachmentIDs resolves PATCH's mixed attachment list. IDs that match
// files[n] are new uploads; all other IDs must name an existing attachment to
// retain. Omitting attachments therefore removes all current files, matching the
// documented replacement semantics.
func retainedAttachmentIDs(existing []db.Attachment, refs []discord.Attachment, uploads []uploadedImage) ([]int64, error) {
	newIndices := make(map[int]bool, len(uploads))
	for _, u := range uploads {
		newIndices[u.Index] = true
	}
	old := make(map[int64]db.Attachment, len(existing))
	for _, a := range existing {
		old[a.ID] = a
	}
	kept := make([]int64, 0, len(refs))
	seen := make(map[int64]bool, len(refs))
	for _, ref := range refs {
		idx, err := attachmentIndex(ref.ID)
		if err != nil {
			return nil, fmt.Errorf("%w: attachment id", errInvalidAttachment)
		}
		if newIndices[idx] {
			continue
		}
		a, ok := old[int64(idx)]
		if !ok || seen[a.ID] {
			return nil, fmt.Errorf("%w: unknown existing attachment", errInvalidAttachment)
		}
		if ref.Filename != "" && ref.Filename != a.Filename {
			return nil, fmt.Errorf("%w: existing attachment filename", errInvalidAttachment)
		}
		seen[a.ID] = true
		kept = append(kept, a.ID)
	}
	return kept, nil
}

func dbAttachments(uploads []uploadedImage) []db.Attachment {
	out := make([]db.Attachment, 0, len(uploads))
	for _, u := range uploads {
		out = append(out, db.Attachment{
			Filename: u.Filename, Description: u.Description, ContentType: u.ContentType,
			Size: int64(len(u.Data)), Data: u.Data,
		})
	}
	return out
}

func attachmentSummary(uploads []uploadedImage) string {
	names := make([]string, 0, len(uploads))
	for _, u := range uploads {
		names = append(names, u.Filename)
	}
	return "Image attachment: " + strings.Join(names, ", ")
}

var (
	errEmpty             = &emptyError{}
	errInvalidAttachment = errors.New("invalid attachment")
)

type emptyError struct{}

func (e *emptyError) Error() string { return "empty payload" }

func writePayloadError(w http.ResponseWriter, err error) {
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"message": "Request body too large", "code": 50035})
		return
	}
	if errors.Is(err, errEmpty) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"message": "Cannot send an empty message", "code": 50006})
		return
	}
	writeJSON(w, http.StatusBadRequest, map[string]any{"message": "Invalid Form Body", "code": 50035})
}

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
