package messaging

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
)

var (
	DBOnce sync.Once
	DB     *sql.DB
	DBErr  error
)

func getDB() (*sql.DB, error) {
	DBOnce.Do(func() {
		db, err := sql.Open("sqlite", "messages.db")
		if err != nil {
			DBErr = fmt.Errorf("open sqlite db: %w", err)
			return
		}

		if _, err := db.Exec(`PRAGMA busy_timeout = 5000;`); err != nil {
			_ = db.Close()
			DBErr = fmt.Errorf("set sqlite busy_timeout: %w", err)
			return
		}

		if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS messages (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	partner_name TEXT NOT NULL,
	direction TEXT NOT NULL,
	sender TEXT NOT NULL,
	receiver TEXT NOT NULL,
	message_type TEXT NOT NULL,
	service TEXT NOT NULL,
	possible_duplicate TEXT,
	service_code TEXT,
	usage_identifier TEXT,
	sender_reference TEXT NOT NULL,
	tag TEXT,
	priority TEXT NOT NULL,
	distribution_id INTEGER NOT NULL UNIQUE,
	message_cloud_reference TEXT NOT NULL UNIQUE,
	received_at_ms INTEGER NOT NULL,
	raw_text TEXT NOT NULL,
	raw_hash TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS mx_data (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	message_id INTEGER NOT NULL,
	app_header TEXT NOT NULL,
	document TEXT NOT NULL,
	FOREIGN KEY (message_id) REFERENCES messages(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS mt_data (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	message_id INTEGER NOT NULL,
	field_tag TEXT NOT NULL,
	field_value TEXT NOT NULL,
	seq_no INTEGER NOT NULL,
	FOREIGN KEY (message_id) REFERENCES messages(id) ON DELETE CASCADE
);
`); err != nil {
			_ = db.Close()
			DBErr = fmt.Errorf("init sqlite schema: %w", err)
			return
		}

		DB = db
	})

	if DBErr != nil {
		return nil, DBErr
	}
	if DB == nil {
		return nil, errors.New("sqlite db not initialized")
	}

	return DB, nil
}

func sha256Hex(v string) string {
	h := sha256.Sum256([]byte(v))
	return hex.EncodeToString(h[:])
}

func nullableText(v string) any {
	if v == "" {
		return nil
	}
	return v
}
