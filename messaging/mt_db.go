package messaging

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

var (
	mtDBOnce sync.Once
	mtDB     *sql.DB
	mtDBErr  error
)

func getMTDB() (*sql.DB, error) {
	mtDBOnce.Do(func() {
		db, err := sql.Open("sqlite", "messages.db")
		if err != nil {
			mtDBErr = fmt.Errorf("open sqlite db: %w", err)
			return
		}

		if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS mt_messages (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	partner_name TEXT NOT NULL,
	direction TEXT NOT NULL,
	sender TEXT NOT NULL,
	receiver TEXT NOT NULL,
	type TEXT NOT NULL,
	sender_reference TEXT NOT NULL,
	priority TEXT NOT NULL,
	distribution_id INTEGER NOT NULL UNIQUE,
	received_at_ms INTEGER NOT NULL,
	raw_text TEXT NOT NULL,
	raw_hash TEXT NOT NULL,
	parse_ok INTEGER NOT NULL,
	parse_error TEXT
);

CREATE TABLE IF NOT EXISTS mt_fields (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	message_id INTEGER NOT NULL,
	field_tag TEXT NOT NULL,
	field_value TEXT NOT NULL,
	seq_no INTEGER NOT NULL,
	FOREIGN KEY (message_id) REFERENCES mt_messages(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_mt_messages_distribution_id ON mt_messages(distribution_id);
CREATE INDEX IF NOT EXISTS idx_mt_fields_message_id ON mt_fields(message_id);
CREATE INDEX IF NOT EXISTS idx_mt_fields_tag ON mt_fields(field_tag);
`); err != nil {
			_ = db.Close()
			mtDBErr = fmt.Errorf("init sqlite schema: %w", err)
			return
		}

		mtDB = db
	})

	if mtDBErr != nil {
		return nil, mtDBErr
	}
	if mtDB == nil {
		return nil, errors.New("sqlite db not initialized")
	}

	return mtDB, nil
}

func WriteMTMessageToSQL(message MTDownload, parseErr error, partnerName string) error {
	db, err := getMTDB()
	if err != nil {
		return err
	}

	rawText := message.Message.Payload
	rawHash := sha256Hex(rawText)

	parseErrText := ""
	if parseErr != nil {
		parseErrText = parseErr.Error()
	}
	parseOK := 0
	if parseErrText == "" {
		parseOK = 1
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	nowMs := time.Now().UnixMilli()
	_, err = tx.Exec(`
INSERT INTO mt_messages (partner_name, direction, distribution_id, sender, receiver, type, sender_reference, priority, received_at_ms, raw_text, raw_hash, parse_ok, parse_error)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(distribution_id) DO UPDATE SET
	partner_name = excluded.partner_name,
	direction = excluded.direction,
	sender = excluded.sender,
	receiver = excluded.receiver,
	type = excluded.type,
	sender_reference = excluded.sender_reference,
	priority = excluded.priority,
	received_at_ms = excluded.received_at_ms,
	raw_text = excluded.raw_text,
	raw_hash = excluded.raw_hash,
	parse_ok = excluded.parse_ok,
	parse_error = excluded.parse_error
`, partnerName, message.Message.Direction, message.Distribution.ID, message.Message.Sender, message.Message.Receiver, message.Message.MessageType, message.Message.SenderReference, message.Message.NetworkInfo.NetworkPriority, nowMs, rawText, rawHash, parseOK, nullableText(parseErrText))
	if err != nil {
		return fmt.Errorf("upsert mt_messages: %w", err)
	}

	var messageID int64
	err = tx.QueryRow(`SELECT id FROM mt_messages WHERE distribution_id = ?`, message.Distribution.ID).Scan(&messageID)
	if err != nil {
		return fmt.Errorf("query message id: %w", err)
	}

	_, err = tx.Exec(`DELETE FROM mt_fields WHERE message_id = ?`, messageID)
	if err != nil {
		return fmt.Errorf("clear mt_fields: %w", err)
	}

	if parseOK == 1 {
		stmt, stmtErr := tx.Prepare(`
INSERT INTO mt_fields (message_id, field_tag, field_value, seq_no)
VALUES (?, ?, ?, ?)
`)
		if stmtErr != nil {
			return fmt.Errorf("prepare mt_fields insert: %w", stmtErr)
		}
		defer stmt.Close()

		for idx, line := range message.Message.MT.Line {
			_, execErr := stmt.Exec(messageID, line.Field, line.Data, idx+1)
			if execErr != nil {
				return fmt.Errorf("insert mt_field seq %d: %w", idx+1, execErr)
			}
		}
	}

	err = tx.Commit()
	if err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}

	return nil
}

func WriteMTAckToSQL(report MTReport, parseErr error, partnerName string) error {
	db, err := getMTDB()
	if err != nil {
		return err
	}

	rawText := report.TransmissionReport.Message.Payload
	rawHash := sha256Hex(rawText)

	parseErrText := ""
	if parseErr != nil {
		parseErrText = parseErr.Error()
	}
	parseOK := 0
	if parseErrText == "" {
		parseOK = 1
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	nowMs := time.Now().UnixMilli()
	_, err = tx.Exec(`
INSERT INTO mt_messages (partner_name, direction, distribution_id, sender, receiver, type, sender_reference, priority, received_at_ms, raw_text, raw_hash, parse_ok, parse_error)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(distribution_id) DO UPDATE SET
	partner_name = excluded.partner_name,
	direction = excluded.direction,
	sender = excluded.sender,
	receiver = excluded.receiver,
	type = excluded.type,
	sender_reference = excluded.sender_reference,
	priority = excluded.priority,
	received_at_ms = excluded.received_at_ms,
	raw_text = excluded.raw_text,
	raw_hash = excluded.raw_hash,
	parse_ok = excluded.parse_ok,
	parse_error = excluded.parse_error
`, partnerName, report.TransmissionReport.Message.Direction, report.Distribution.ID, report.TransmissionReport.Message.Sender, report.TransmissionReport.Message.Receiver, report.TransmissionReport.Message.MessageType, report.TransmissionReport.Message.SenderReference, report.TransmissionReport.Message.NetworkInfo.NetworkPriority, nowMs, rawText, rawHash, parseOK, nullableText(parseErrText))
	if err != nil {
		return fmt.Errorf("upsert mt_messages: %w", err)
	}

	var messageID int64
	err = tx.QueryRow(`SELECT id FROM mt_messages WHERE distribution_id = ?`, report.Distribution.ID).Scan(&messageID)
	if err != nil {
		return fmt.Errorf("query message id: %w", err)
	}

	_, err = tx.Exec(`DELETE FROM mt_fields WHERE message_id = ?`, messageID)
	if err != nil {
		return fmt.Errorf("clear mt_fields: %w", err)
	}

	if parseOK == 1 {
		stmt, stmtErr := tx.Prepare(`
INSERT INTO mt_fields (message_id, field_tag, field_value, seq_no)
VALUES (?, ?, ?, ?)
`)
		if stmtErr != nil {
			return fmt.Errorf("prepare mt_fields insert: %w", stmtErr)
		}
		defer stmt.Close()

		for idx, line := range report.TransmissionReport.Message.MT.Line {
			_, execErr := stmt.Exec(messageID, line.Field, line.Data, idx+1)
			if execErr != nil {
				return fmt.Errorf("insert mt_field seq %d: %w", idx+1, execErr)
			}
		}
	}

	err = tx.Commit()
	if err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}

	return nil
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
