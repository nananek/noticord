package db

import (
	"bytes"
	"database/sql"
	"path/filepath"
	"strconv"
	"testing"
)

func TestOpenMigratesLegacyAttachmentPublicIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	legacy, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec(`
CREATE TABLE messages (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  token_id TEXT,
  channel_id TEXT,
  username TEXT NOT NULL DEFAULT '',
  avatar TEXT NOT NULL DEFAULT '',
  content TEXT NOT NULL DEFAULT '',
  embeds TEXT NOT NULL DEFAULT '',
  raw TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL
);
CREATE TABLE attachments (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  message_id INTEGER NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  filename TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  content_type TEXT NOT NULL,
  size INTEGER NOT NULL,
  data BLOB NOT NULL,
  created_at INTEGER NOT NULL
);
INSERT INTO messages(id,channel_id,username,embeds,raw,created_at) VALUES(1,'legacy','test','[]','{}',1);
INSERT INTO attachments(id,message_id,filename,content_type,size,data,created_at) VALUES(1,1,'pixel.png','image/png',8,X'89504E470D0A1A0A',1);
`); err != nil {
		legacy.Close()
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	m, err := GetMessage(d, 1)
	if err != nil || m == nil || len(m.Attachments) != 1 {
		t.Fatalf("migrated message = %#v, %v", m, err)
	}
	publicID, err := strconv.ParseInt(m.Attachments[0].PublicID, 10, 64)
	if err != nil || publicID < 100000000000000000 {
		t.Fatalf("invalid migrated public ID %q: %v", m.Attachments[0].PublicID, err)
	}
	a, err := GetAttachment(d, m.Attachments[0].ID)
	if err != nil || a == nil || a.PublicID != m.Attachments[0].PublicID || !bytes.Equal(a.Data, []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}) {
		t.Fatalf("migrated attachment = %#v, %v", a, err)
	}
}
