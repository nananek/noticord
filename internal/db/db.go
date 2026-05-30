// Package db はシングルユーザー専用 noticord の SQLite アクセス層。
// DB を所有するのは admin プロセスのみ。WAL + busy_timeout で
// 同時アクセスに耐える。modernc.org/sqlite を使うため cgo 不要。
package db

import (
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS config (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS channels (
  id         TEXT PRIMARY KEY,
  name       TEXT NOT NULL,
  topic      TEXT NOT NULL DEFAULT '',
  position   INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS tokens (
  id           TEXT PRIMARY KEY,
  token        TEXT NOT NULL,
  name         TEXT NOT NULL DEFAULT '',
  channel_id   TEXT,
  created_at   INTEGER NOT NULL,
  last_used_at INTEGER
);

CREATE TABLE IF NOT EXISTS subscriptions (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  endpoint   TEXT NOT NULL UNIQUE,
  p256dh     TEXT NOT NULL,
  auth       TEXT NOT NULL,
  created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS messages (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  token_id   TEXT,
  channel_id TEXT,
  username   TEXT NOT NULL DEFAULT '',
  avatar     TEXT NOT NULL DEFAULT '',
  content    TEXT NOT NULL DEFAULT '',
  embeds     TEXT NOT NULL DEFAULT '',
  raw        TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_messages_created ON messages(created_at DESC);
`

// Open は SQLite を開き、スキーマ適用と旧DBのマイグレーションを行う。
func Open(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)", path)
	d, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// SQLite はプロセス内では単一書き込みが安全。単一ユーザー用途なので接続数を絞り
	// "database is locked" を避ける(プロセス間は busy_timeout が吸収する)。
	d.SetMaxOpenConns(1)
	if _, err := d.Exec(schema); err != nil {
		d.Close()
		return nil, err
	}
	if err := migrate(d); err != nil {
		d.Close()
		return nil, err
	}
	return d, nil
}

// hasColumn は table に col 列が存在するかを PRAGMA table_info で判定する。
func hasColumn(d *sql.DB, table, col string) (bool, error) {
	rows, err := d.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			cid       int
			name      string
			ctype     string
			notnull   int
			dfltValue any
			pk        int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			return false, err
		}
		if name == col {
			return true, nil
		}
	}
	return false, rows.Err()
}

// migrate は旧スキーマ(channels 無し・channel_id 無し)の既存DBを安全に更新する。
// SQLite は列の IF NOT EXISTS 不可なので存在判定してから ADD COLUMN する。
// 既存トークン/履歴は失わず、既定チャンネル #general へ移行する。
func migrate(d *sql.DB) error {
	// 旧DB向けの欠落列追加(新規DBでは schema が既に持つので no-op)。
	for _, m := range []struct{ table, col, ddl string }{
		{"tokens", "channel_id", "ALTER TABLE tokens ADD COLUMN channel_id TEXT"},
		{"messages", "channel_id", "ALTER TABLE messages ADD COLUMN channel_id TEXT"},
		{"messages", "avatar", "ALTER TABLE messages ADD COLUMN avatar TEXT NOT NULL DEFAULT ''"},
	} {
		ok, err := hasColumn(d, m.table, m.col)
		if err != nil {
			return err
		}
		if !ok {
			if _, err := d.Exec(m.ddl); err != nil {
				return err
			}
		}
	}

	// channel_id 列が確実に存在してから(旧DBでは上の ALTER 後)インデックスを張る。
	if _, err := d.Exec("CREATE INDEX IF NOT EXISTS idx_messages_channel ON messages(channel_id, created_at DESC)"); err != nil {
		return err
	}

	// 既定チャンネルを保証し、未割り当てのトークン/メッセージを移行する。
	def, err := EnsureDefaultChannel(d)
	if err != nil {
		return err
	}
	if _, err := d.Exec("UPDATE tokens SET channel_id=? WHERE channel_id IS NULL OR channel_id=''", def); err != nil {
		return err
	}
	if _, err := d.Exec("UPDATE messages SET channel_id=? WHERE channel_id IS NULL OR channel_id=''", def); err != nil {
		return err
	}
	return nil
}

// ---- 乱数ユーティリティ ----

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// newID は Discord の snowflake に見た目を寄せた 18 桁の数値文字列を返す。
func newID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	n := binary.BigEndian.Uint64(b)
	id := n%900000000000000000 + 100000000000000000
	return strconv.FormatUint(id, 10)
}

// ---- config ----

func GetConfig(d *sql.DB, key string) (string, error) {
	var v string
	err := d.QueryRow("SELECT value FROM config WHERE key=?", key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
}

func SetConfig(d *sql.DB, key, value string) error {
	_, err := d.Exec(
		"INSERT INTO config(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value",
		key, value)
	return err
}

// VAPID は Web Push の鍵ペアと subject。
type VAPID struct {
	Public  string
	Private string
	Subject string
}

// EnsureVAPID は鍵が無ければ生成して永続化する。INSERT OR IGNORE で原子的に確定する。
func EnsureVAPID(d *sql.DB, subject string) (VAPID, error) {
	priv, pub, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		return VAPID{}, err
	}
	tx, err := d.Begin()
	if err != nil {
		return VAPID{}, err
	}
	// 既に存在すれば無視される(=最初の書き込みが勝つ)。
	if _, err := tx.Exec("INSERT OR IGNORE INTO config(key,value) VALUES('vapid_private',?)", priv); err != nil {
		tx.Rollback()
		return VAPID{}, err
	}
	if _, err := tx.Exec("INSERT OR IGNORE INTO config(key,value) VALUES('vapid_public',?)", pub); err != nil {
		tx.Rollback()
		return VAPID{}, err
	}
	if subject != "" {
		if _, err := tx.Exec("INSERT OR IGNORE INTO config(key,value) VALUES('vapid_subject',?)", subject); err != nil {
			tx.Rollback()
			return VAPID{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return VAPID{}, err
	}
	return LoadVAPID(d)
}

// LoadVAPID は保存済みの鍵を読み出す(未生成なら空文字)。
func LoadVAPID(d *sql.DB) (VAPID, error) {
	var v VAPID
	var err error
	if v.Public, err = GetConfig(d, "vapid_public"); err != nil {
		return v, err
	}
	if v.Private, err = GetConfig(d, "vapid_private"); err != nil {
		return v, err
	}
	if v.Subject, err = GetConfig(d, "vapid_subject"); err != nil {
		return v, err
	}
	if v.Subject == "" {
		v.Subject = "mailto:admin@example.com"
	}
	return v, nil
}

// ---- channels ----

type Channel struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Topic         string `json:"topic"`
	Position      int    `json:"position"`
	CreatedAt     int64  `json:"created_at"`
	WebhookCount  int    `json:"webhook_count"`
	LastMessageAt int64  `json:"last_message_at"`
}

// EnsureDefaultChannel はチャンネルが1つも無ければ #general を作り、その ID を返す。
// 既にあれば最も古い(position, created_at)チャンネルの ID を返す。
func EnsureDefaultChannel(d *sql.DB) (string, error) {
	var id string
	err := d.QueryRow("SELECT id FROM channels ORDER BY position, created_at LIMIT 1").Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return "", err
	}
	c, err := CreateChannel(d, "general", "")
	if err != nil {
		return "", err
	}
	return c.ID, nil
}

func CreateChannel(d *sql.DB, name, topic string) (Channel, error) {
	var pos int
	// 末尾に追加するため現在の最大 position+1 を採番する。
	_ = d.QueryRow("SELECT COALESCE(MAX(position),-1)+1 FROM channels").Scan(&pos)
	c := Channel{ID: newID(), Name: name, Topic: topic, Position: pos, CreatedAt: time.Now().Unix()}
	_, err := d.Exec("INSERT INTO channels(id,name,topic,position,created_at) VALUES(?,?,?,?,?)",
		c.ID, c.Name, c.Topic, c.Position, c.CreatedAt)
	return c, err
}

// ListChannels は各チャンネルの webhook 数と最終メッセージ時刻を集計して返す。
func ListChannels(d *sql.DB) ([]Channel, error) {
	rows, err := d.Query(`
		SELECT c.id, c.name, c.topic, c.position, c.created_at,
		       (SELECT COUNT(*) FROM tokens t WHERE t.channel_id = c.id),
		       COALESCE((SELECT MAX(m.created_at) FROM messages m WHERE m.channel_id = c.id), 0)
		FROM channels c
		ORDER BY c.position, c.created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Channel
	for rows.Next() {
		var c Channel
		if err := rows.Scan(&c.ID, &c.Name, &c.Topic, &c.Position, &c.CreatedAt, &c.WebhookCount, &c.LastMessageAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func GetChannel(d *sql.DB, id string) (*Channel, error) {
	var c Channel
	err := d.QueryRow("SELECT id,name,topic,position,created_at FROM channels WHERE id=?", id).
		Scan(&c.ID, &c.Name, &c.Topic, &c.Position, &c.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func UpdateChannel(d *sql.DB, id, name, topic string) error {
	_, err := d.Exec("UPDATE channels SET name=?, topic=? WHERE id=?", name, topic, id)
	return err
}

// DeleteChannel はチャンネルと、それに紐付くトークン・メッセージをまとめて削除する。
func DeleteChannel(d *sql.DB, id string) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	for _, q := range []string{
		"DELETE FROM messages WHERE channel_id=?",
		"DELETE FROM tokens WHERE channel_id=?",
		"DELETE FROM channels WHERE id=?",
	} {
		if _, err := tx.Exec(q, id); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func CountChannels(d *sql.DB) (int, error) {
	var n int
	err := d.QueryRow("SELECT COUNT(*) FROM channels").Scan(&n)
	return n, err
}

// ---- tokens ----

type Token struct {
	ID         string `json:"id"`
	Token      string `json:"token"`
	Name       string `json:"name"`
	ChannelID  string `json:"channel_id"`
	CreatedAt  int64  `json:"created_at"`
	LastUsedAt int64  `json:"last_used_at"`
}

func CreateToken(d *sql.DB, channelID, name string) (Token, error) {
	t := Token{ID: newID(), Token: randHex(32), Name: name, ChannelID: channelID, CreatedAt: time.Now().Unix()}
	_, err := d.Exec("INSERT INTO tokens(id,token,name,channel_id,created_at) VALUES(?,?,?,?,?)",
		t.ID, t.Token, t.Name, t.ChannelID, t.CreatedAt)
	return t, err
}

const tokenCols = "id,token,name,COALESCE(channel_id,''),created_at,COALESCE(last_used_at,0)"

func scanToken(s interface{ Scan(...any) error }) (Token, error) {
	var t Token
	err := s.Scan(&t.ID, &t.Token, &t.Name, &t.ChannelID, &t.CreatedAt, &t.LastUsedAt)
	return t, err
}

func ListTokensByChannel(d *sql.DB, channelID string) ([]Token, error) {
	rows, err := d.Query("SELECT "+tokenCols+" FROM tokens WHERE channel_id=? ORDER BY created_at DESC", channelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Token
	for rows.Next() {
		t, err := scanToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func GetToken(d *sql.DB, id string) (*Token, error) {
	t, err := scanToken(d.QueryRow("SELECT "+tokenCols+" FROM tokens WHERE id=?", id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func DeleteToken(d *sql.DB, id string) error {
	_, err := d.Exec("DELETE FROM tokens WHERE id=?", id)
	return err
}

func TouchToken(d *sql.DB, id string) error {
	_, err := d.Exec("UPDATE tokens SET last_used_at=? WHERE id=?", time.Now().Unix(), id)
	return err
}

// ---- subscriptions ----

type Subscription struct {
	ID        int64  `json:"id"`
	Endpoint  string `json:"endpoint"`
	P256dh    string `json:"p256dh"`
	Auth      string `json:"auth"`
	CreatedAt int64  `json:"created_at"`
}

func AddSubscription(d *sql.DB, endpoint, p256dh, auth string) error {
	_, err := d.Exec(`INSERT INTO subscriptions(endpoint,p256dh,auth,created_at) VALUES(?,?,?,?)
		ON CONFLICT(endpoint) DO UPDATE SET p256dh=excluded.p256dh, auth=excluded.auth`,
		endpoint, p256dh, auth, time.Now().Unix())
	return err
}

func ListSubscriptions(d *sql.DB) ([]Subscription, error) {
	rows, err := d.Query("SELECT id,endpoint,p256dh,auth,created_at FROM subscriptions ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Subscription
	for rows.Next() {
		var s Subscription
		if err := rows.Scan(&s.ID, &s.Endpoint, &s.P256dh, &s.Auth, &s.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func DeleteSubscriptionByEndpoint(d *sql.DB, endpoint string) error {
	_, err := d.Exec("DELETE FROM subscriptions WHERE endpoint=?", endpoint)
	return err
}

// ---- messages ----

type Message struct {
	ID        int64  `json:"id"`
	TokenID   string `json:"token_id"`
	ChannelID string `json:"channel_id"`
	Username  string `json:"username"`
	Avatar    string `json:"avatar"`
	Content   string `json:"content"`
	Embeds    string `json:"embeds"`
	Raw       string `json:"raw"`
	CreatedAt int64  `json:"created_at"`
}

func AddMessage(d *sql.DB, m Message) (int64, error) {
	if m.CreatedAt == 0 {
		m.CreatedAt = time.Now().Unix()
	}
	res, err := d.Exec(
		"INSERT INTO messages(token_id,channel_id,username,avatar,content,embeds,raw,created_at) VALUES(?,?,?,?,?,?,?,?)",
		m.TokenID, m.ChannelID, m.Username, m.Avatar, m.Content, m.Embeds, m.Raw, m.CreatedAt)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

const messageCols = "id,COALESCE(token_id,''),COALESCE(channel_id,''),username,avatar,content,embeds,raw,created_at"

func scanMessages(rows *sql.Rows) ([]Message, error) {
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.TokenID, &m.ChannelID, &m.Username, &m.Avatar, &m.Content, &m.Embeds, &m.Raw, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func clampLimit(limit int) int {
	if limit <= 0 || limit > 500 {
		return 100
	}
	return limit
}

func ListMessagesByChannel(d *sql.DB, channelID string, limit int) ([]Message, error) {
	rows, err := d.Query(
		"SELECT "+messageCols+" FROM messages WHERE channel_id=? ORDER BY created_at DESC, id DESC LIMIT ?",
		channelID, clampLimit(limit))
	if err != nil {
		return nil, err
	}
	return scanMessages(rows)
}

func DeleteMessage(d *sql.DB, id int64) error {
	_, err := d.Exec("DELETE FROM messages WHERE id=?", id)
	return err
}

// ClearMessagesByChannel はチャンネル内の全メッセージを削除する。
func ClearMessagesByChannel(d *sql.DB, channelID string) error {
	_, err := d.Exec("DELETE FROM messages WHERE channel_id=?", channelID)
	return err
}

// PruneMessages は最新 keep 件だけ残して古いものを削除する(チャンネル横断の総量上限)。
func PruneMessages(d *sql.DB, keep int) error {
	if keep <= 0 {
		return nil
	}
	_, err := d.Exec(
		`DELETE FROM messages WHERE id NOT IN (SELECT id FROM messages ORDER BY created_at DESC, id DESC LIMIT ?)`,
		keep)
	return err
}
