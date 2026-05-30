// Package db はシングルユーザー専用 noticord の SQLite アクセス層。
// admin / webhook の2プロセスが同一ファイルを共有するため WAL + busy_timeout で
// クロスプロセスの同時アクセスに耐える。modernc.org/sqlite を使うため cgo 不要。
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

CREATE TABLE IF NOT EXISTS tokens (
  id           TEXT PRIMARY KEY,
  token        TEXT NOT NULL,
  name         TEXT NOT NULL DEFAULT '',
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
  username   TEXT NOT NULL DEFAULT '',
  content    TEXT NOT NULL DEFAULT '',
  embeds     TEXT NOT NULL DEFAULT '',
  raw        TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_messages_created ON messages(created_at DESC);
`

// Open は SQLite を開きスキーマを適用する。
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
	return d, nil
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

// EnsureVAPID は鍵が無ければ生成して永続化する。admin / webhook の双方から呼ばれても
// 1トランザクション内の INSERT OR IGNORE で原子的に確定し、両プロセスが同じ鍵を読む。
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

// ---- tokens ----

type Token struct {
	ID         string `json:"id"`
	Token      string `json:"token"`
	Name       string `json:"name"`
	CreatedAt  int64  `json:"created_at"`
	LastUsedAt int64  `json:"last_used_at"`
}

func CreateToken(d *sql.DB, name string) (Token, error) {
	t := Token{ID: newID(), Token: randHex(32), Name: name, CreatedAt: time.Now().Unix()}
	_, err := d.Exec("INSERT INTO tokens(id,token,name,created_at) VALUES(?,?,?,?)",
		t.ID, t.Token, t.Name, t.CreatedAt)
	return t, err
}

func ListTokens(d *sql.DB) ([]Token, error) {
	rows, err := d.Query("SELECT id,token,name,created_at,COALESCE(last_used_at,0) FROM tokens ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Token
	for rows.Next() {
		var t Token
		if err := rows.Scan(&t.ID, &t.Token, &t.Name, &t.CreatedAt, &t.LastUsedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func GetToken(d *sql.DB, id string) (*Token, error) {
	var t Token
	err := d.QueryRow("SELECT id,token,name,created_at,COALESCE(last_used_at,0) FROM tokens WHERE id=?", id).
		Scan(&t.ID, &t.Token, &t.Name, &t.CreatedAt, &t.LastUsedAt)
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
	Username  string `json:"username"`
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
		"INSERT INTO messages(token_id,username,content,embeds,raw,created_at) VALUES(?,?,?,?,?,?)",
		m.TokenID, m.Username, m.Content, m.Embeds, m.Raw, m.CreatedAt)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func ListMessages(d *sql.DB, limit int) ([]Message, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := d.Query(
		"SELECT id,COALESCE(token_id,''),username,content,embeds,raw,created_at FROM messages ORDER BY created_at DESC, id DESC LIMIT ?",
		limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.TokenID, &m.Username, &m.Content, &m.Embeds, &m.Raw, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func DeleteMessage(d *sql.DB, id int64) error {
	_, err := d.Exec("DELETE FROM messages WHERE id=?", id)
	return err
}

func ClearMessages(d *sql.DB) error {
	_, err := d.Exec("DELETE FROM messages")
	return err
}

// PruneMessages は最新 keep 件だけ残して古いものを削除する。
func PruneMessages(d *sql.DB, keep int) error {
	if keep <= 0 {
		return nil
	}
	_, err := d.Exec(
		`DELETE FROM messages WHERE id NOT IN (SELECT id FROM messages ORDER BY created_at DESC, id DESC LIMIT ?)`,
		keep)
	return err
}
