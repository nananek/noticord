// Package config は環境変数からの設定読み出しをまとめる。
package config

import (
	"os"
	"strings"
)

// Env は環境変数を返し、未設定/空なら def を返す。
func Env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// Admin は画面サーバー兼受信処理の設定。DB・VAPID・Web Push 送信をすべて所有する。
type Admin struct {
	DBPath       string // SQLite ファイルパス
	Listen       string // PWA/管理 API の TCP 待ち受け(tailscale netns 上)
	IngestSocket string // Discord 互換受信を listen する UDS パス(cloudflared が接続)
	Password     string // 管理画面ログインパスワード(空なら認証無効=Tailscale信頼)
	VAPIDSubject string // VAPID subject (mailto: など)
	PublicURL    string // cloudflared で公開する webhook のベース URL
	KeepMessages int    // 履歴の最大保持件数(0で無制限)
	Debug        bool   // 受信・プッシュ送信の詳細ログを出すか(NOTICORD_DEBUG)
	Lang         string // UI 言語 "en"/"ja"(NOTICORD_LANG。未知の値は en へ倒す)
}

func LoadAdmin() Admin {
	return Admin{
		DBPath:       Env("NOTICORD_DB", "/data/noticord.db"),
		Listen:       Env("NOTICORD_LISTEN", "127.0.0.1:8080"),
		IngestSocket: Env("NOTICORD_INGEST_SOCKET", "/run/noticord/ingest.sock"),
		Password:     Env("NOTICORD_ADMIN_PASSWORD", ""),
		VAPIDSubject: Env("NOTICORD_VAPID_SUBJECT", "mailto:admin@example.com"),
		PublicURL:    strings.TrimRight(Env("NOTICORD_PUBLIC_URL", ""), "/"),
		KeepMessages: 1000,
		Debug:        Truthy(Env("NOTICORD_DEBUG", "")),
		Lang:         NormalizeLang(Env("NOTICORD_LANG", "en")),
	}
}

// NormalizeLang は対応言語(en/ja)に丸める。未対応の値はすべて en。
func NormalizeLang(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "ja", "ja-jp", "jp", "japanese":
		return "ja"
	default:
		return "en"
	}
}

// Truthy は環境変数文字列を真偽に解釈する(1/true/yes/on を真)。
func Truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
