package messaging

import (
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	mxDBOnce sync.Once
	mxDB     *sql.DB
	mxDBErr  error
)

func getMXDB() (*sql.DB, error) {
	mxDBOnce.Do(func() {
		db, err := sql.Open("sqlite", "messages.db")
		if err != nil {
			mxDBErr = fmt.Errorf("open sqlite db: %w", err)
			return
		}

		if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS mx_messages (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	partner_name TEXT NOT NULL,
	direction TEXT NOT NULL,
	requestor TEXT NOT NULL,
	responder TEXT NOT NULL,
	type TEXT NOT NULL,
	sender_reference TEXT NOT NULL,
	priority TEXT NOT NULL,
	distribution_id INTEGER NOT NULL UNIQUE,
	received_at_ms INTEGER NOT NULL,
	raw_text TEXT NOT NULL,
	raw_hash TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS mx_data (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	message_id INTEGER NOT NULL,
	app_header TEXT NOT NULL,
	document TEXT NOT NULL,
	FOREIGN KEY (message_id) REFERENCES mx_messages(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_mx_messages_distribution_id ON mx_messages(distribution_id);
CREATE INDEX IF NOT EXISTS idx_mx_data_message_id ON mx_data(message_id);
CREATE INDEX IF NOT EXISTS idx_mx_data_app_header ON mx_data(app_header);
`); err != nil {
			_ = db.Close()
			mxDBErr = fmt.Errorf("init sqlite schema: %w", err)
			return
		}

		mxDB = db
	})

	if mxDBErr != nil {
		return nil, mxDBErr
	}
	if mxDB == nil {
		return nil, errors.New("sqlite db not initialized")
	}

	return mxDB, nil
}

func WriteMXMessageToSQL(message MXDownload, partnerName string) error {
	db, err := getMXDB()
	if err != nil {
		return fmt.Errorf("getMXDB: %w", err)
	}

	rawText := message.Message.Payload
	rawHash := sha256Hex(rawText)

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		} else {
			err = tx.Commit()
		}
	}()

	nowMs := time.Now().UnixMilli()
	_, err = tx.Exec(`
INSERT INTO mx_messages (partner_name, direction, requestor, responder, type, sender_reference, priority, distribution_id, received_at_ms, raw_text, raw_hash)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(distribution_id) DO UPDATE SET
	partner_name=excluded.partner_name,
	direction=excluded.direction,
	requestor=excluded.requestor,
	responder=excluded.responder,
	type=excluded.type,
	sender_reference=excluded.sender_reference,
	priority=excluded.priority,
	received_at_ms=excluded.received_at_ms,
	raw_text=excluded.raw_text,
	raw_hash=excluded.raw_hash
`, partnerName, message.Message.Direction, message.Message.Requestor, message.Message.Responder, message.Message.MessageType, message.Message.SenderReference, message.Message.NetworkInfo.ServiceCode, message.Distribution.ID, nowMs, rawText, rawHash)
	if err != nil {
		return fmt.Errorf("insert/update mx_messages: %w", err)
	}

	var messageID int64
	err = tx.QueryRow(`SELECT id FROM mx_messages WHERE distribution_id = ?`, message.Distribution.ID).Scan(&messageID)
	if err != nil {
		return fmt.Errorf("get mx_message id: %w", err)
	}

	_, err = tx.Exec(`DELETE FROM mx_data WHERE message_id = ?`, messageID)
	if err != nil {
		return fmt.Errorf("delete existing mx_data: %w", err)
	}

	_, err = tx.Exec(`INSERT INTO mx_data (message_id, app_header, document) VALUES (?, ?, ?)`, messageID, message.Message.MX.AppHeader, message.Message.MX.Document)
	if err != nil {
		return fmt.Errorf("insert mx_data: %w", err)
	}

	err = tx.Commit()
	if err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

func WriteMXReportToSQL(report MXReport, partnerName string) error {
	db, err := getMXDB()
	if err != nil {
		return fmt.Errorf("getMXDB: %w", err)
	}

	rawText := report.TransmissionReport.Message.Payload
	rawHash := sha256Hex(rawText)

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		} else {
			err = tx.Commit()
		}
	}()

	nowMs := time.Now().UnixMilli()
	_, err = tx.Exec(`
INSERT INTO mx_messages (partner_name, direction, requestor, responder, type, sender_reference, priority, distribution_id, received_at_ms, raw_text, raw_hash)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(distribution_id) DO UPDATE SET
	partner_name=excluded.partner_name,
	direction=excluded.direction,
	requestor=excluded.requestor,
	responder=excluded.responder,
	type=excluded.type,
	sender_reference=excluded.sender_reference,
	priority=excluded.priority,
	received_at_ms=excluded.received_at_ms,
	raw_text=excluded.raw_text,
	raw_hash=excluded.raw_hash
`, partnerName, report.TransmissionReport.Message.Direction, report.TransmissionReport.Message.Requestor, report.TransmissionReport.Message.Responder, report.TransmissionReport.Message.MessageType, report.TransmissionReport.Message.SenderReference, report.TransmissionReport.Message.NetworkInfo.NetworkPriority, report.Distribution.ID, nowMs, rawText, rawHash)
	if err != nil {
		return fmt.Errorf("insert/update mx_messages: %w", err)
	}

	var messageID int64
	err = tx.QueryRow(`SELECT id FROM mx_messages WHERE distribution_id = ?`, report.Distribution.ID).Scan(&messageID)
	if err != nil {
		return fmt.Errorf("get mx_message id: %w", err)
	}

	_, err = tx.Exec(`DELETE FROM mx_data WHERE message_id = ?`, messageID)
	if err != nil {
		return fmt.Errorf("delete existing mx_data: %w", err)
	}

	_, err = tx.Exec(`INSERT INTO mx_data (message_id, app_header, document) VALUES (?, ?, ?)`, messageID, report.TransmissionReport.Message.MX.AppHeader, report.TransmissionReport.Message.MX.Document)
	if err != nil {
		return fmt.Errorf("insert mx_data: %w", err)
	}

	err = tx.Commit()
	if err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}
