// Package discord は Discord 互換 Webhook のペイロードを解釈する。
// embed をできる限り忠実に保持し、PWA 側で Discord 風にレンダリングできるようにする。
package discord

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

type EmbedFooter struct {
	Text    string `json:"text,omitempty"`
	IconURL string `json:"icon_url,omitempty"`
}

type EmbedImage struct {
	URL    string `json:"url,omitempty"`
	Height int    `json:"height,omitempty"`
	Width  int    `json:"width,omitempty"`
}

type EmbedAuthor struct {
	Name    string `json:"name,omitempty"`
	URL     string `json:"url,omitempty"`
	IconURL string `json:"icon_url,omitempty"`
}

type EmbedField struct {
	Name   string `json:"name,omitempty"`
	Value  string `json:"value,omitempty"`
	Inline bool   `json:"inline,omitempty"`
}

// AttachmentID は Discord が許す number / string の attachment id を同一の
// 文字列表現で扱う。Webhook の PATCH では既存添付 ID と files[n] の index
// が同じ attachments 配列に混在するため、精度を失う float64 は使わない。
type AttachmentID string

func (id *AttachmentID) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 {
		return fmt.Errorf("attachment id is empty")
	}
	var s string
	if b[0] == '"' {
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
	} else {
		dec := json.NewDecoder(bytes.NewReader(b))
		dec.UseNumber()
		var n json.Number
		if err := dec.Decode(&n); err != nil {
			return fmt.Errorf("attachment id must be a string or number: %w", err)
		}
		s = n.String()
	}
	if strings.TrimSpace(s) == "" {
		return fmt.Errorf("attachment id is empty")
	}
	*id = AttachmentID(s)
	return nil
}

// Attachment は Discord の payload_json 内で file と対応付ける partial
// attachment。受信済みメッセージの添付モデルは internal/db 側に置く。
type Attachment struct {
	ID          AttachmentID `json:"id"`
	Filename    string       `json:"filename,omitempty"`
	Description string       `json:"description,omitempty"`
}

// Embed は Discord の embed オブジェクト(主要フィールドを網羅)。
type Embed struct {
	Title       string       `json:"title,omitempty"`
	Type        string       `json:"type,omitempty"`
	Description string       `json:"description,omitempty"`
	URL         string       `json:"url,omitempty"`
	Timestamp   string       `json:"timestamp,omitempty"`
	Color       int          `json:"color,omitempty"`
	Footer      *EmbedFooter `json:"footer,omitempty"`
	Image       *EmbedImage  `json:"image,omitempty"`
	Thumbnail   *EmbedImage  `json:"thumbnail,omitempty"`
	Author      *EmbedAuthor `json:"author,omitempty"`
	Fields      []EmbedField `json:"fields,omitempty"`
}

type Payload struct {
	Content     string       `json:"content,omitempty"`
	Username    string       `json:"username,omitempty"`
	AvatarURL   string       `json:"avatar_url,omitempty"`
	Embeds      []Embed      `json:"embeds,omitempty"`
	Attachments []Attachment `json:"attachments,omitempty"`
}

// Notification は OS 通知に表示するタイトルと本文を組み立てる。
// 通知はプレーンテキストしか出せないため、embed は要約してテキスト化する。
func (p Payload) Notification(fallbackTitle string) (title, body string) {
	title = strings.TrimSpace(p.Username)
	if title == "" {
		title = fallbackTitle
	}
	body = strings.TrimSpace(p.Content)
	if body == "" && len(p.Embeds) > 0 {
		e := p.Embeds[0]
		var parts []string
		if e.Author != nil && e.Author.Name != "" {
			parts = append(parts, e.Author.Name)
		}
		if e.Title != "" {
			parts = append(parts, e.Title)
		}
		if e.Description != "" {
			parts = append(parts, e.Description)
		}
		if len(parts) == 0 {
			for _, f := range e.Fields {
				parts = append(parts, f.Name+": "+f.Value)
			}
		}
		body = strings.Join(parts, "\n")
	}
	if body == "" {
		body = "(空のメッセージ)"
	}
	return title, body
}
