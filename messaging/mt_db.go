package messaging

import (
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

func WriteMTMessageToSQL(message MTDownload, partnerName string) error {
	db, err := getDB()
	if err != nil {
		return err
	}

	rawText := message.Message.Payload
	rawHash := sha256Hex(rawText)

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
INSERT INTO messages (partner_name, direction, sender, receiver, message_type,
service, possible_duplicate, sender_reference, tag, priority, 
distribution_id, message_cloud_reference, received_at_ms, raw_text, raw_hash)

VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)

ON CONFLICT(distribution_id) DO UPDATE SET
	partner_name = excluded.partner_name,
	direction = excluded.direction,
	sender = excluded.sender,
	receiver = excluded.receiver,
	message_type = excluded.message_type,
	service = excluded.service,
	possible_duplicate = excluded.possible_duplicate,
	sender_reference = excluded.sender_reference,
	tag = excluded.tag,
	priority = excluded.priority,
	distribution_id = excluded.distribution_id,
	message_cloud_reference = excluded.message_cloud_reference,
	received_at_ms = excluded.received_at_ms,
	raw_text = excluded.raw_text,
	raw_hash = excluded.raw_hash
`, partnerName, message.Message.Direction, message.Message.Sender, message.Message.Receiver,
		message.Message.MessageType, message.Distribution.Service, message.Distribution.PossibleDuplicate, message.Message.SenderReference,
		message.Distribution.DistributionTag, message.Message.NetworkInfo.NetworkPriority, message.Distribution.ID,
		message.Distribution.CloudReference, nowMs, rawText, rawHash)
	if err != nil {
		return fmt.Errorf("upsert messages: %w", err)
	}

	var messageID int64
	err = tx.QueryRow(`SELECT id FROM messages WHERE distribution_id = ?`, message.Distribution.ID).Scan(&messageID)
	if err != nil {
		return fmt.Errorf("query message id: %w", err)
	}

	_, err = tx.Exec(`DELETE FROM mt_data WHERE message_id = ?`, messageID)
	if err != nil {
		return fmt.Errorf("clear mt_data: %w", err)
	}

	stmt, stmtErr := tx.Prepare(`
	INSERT INTO mt_data (message_id, field_tag, field_value, seq_no)
	VALUES (?, ?, ?, ?)
	`)
	if stmtErr != nil {
		return fmt.Errorf("prepare mt_data insert: %w", stmtErr)
	}
	defer stmt.Close()

	for idx, line := range message.Message.MT.Line {
		_, execErr := stmt.Exec(messageID, line.Field, line.Data, idx+1)
		if execErr != nil {
			return fmt.Errorf("insert mt_data seq %d: %w", idx+1, execErr)
		}
	}

	err = tx.Commit()
	if err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}

	return nil
}

func WriteMTReportToSQL(report MTReport, partnerName string) error {
	db, err := getDB()
	if err != nil {
		return err
	}

	rawText := report.TransmissionReport.Message.Payload
	rawHash := sha256Hex(rawText)

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
INSERT INTO messages (partner_name, direction, sender, receiver, message_type,
service, possible_duplicate, sender_reference, tag, priority, 
distribution_id, message_cloud_reference, received_at_ms, raw_text, raw_hash)

VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)

ON CONFLICT(distribution_id) DO UPDATE SET
	partner_name = excluded.partner_name,
	direction = excluded.direction,
	sender = excluded.sender,
	receiver = excluded.receiver,
	message_type = excluded.message_type,
	service = excluded.service,
	possible_duplicate = excluded.possible_duplicate,
	sender_reference = excluded.sender_reference,
	tag = excluded.tag,
	priority = excluded.priority,
	distribution_id = excluded.distribution_id,
	message_cloud_reference = excluded.message_cloud_reference,
	received_at_ms = excluded.received_at_ms,
	raw_text = excluded.raw_text,
	raw_hash = excluded.raw_hash
`, partnerName, report.TransmissionReport.Message.Direction, report.TransmissionReport.Message.Sender, report.TransmissionReport.Message.Receiver,
		report.TransmissionReport.Message.MessageType, report.Distribution.Service, report.Distribution.PossibleDuplicate, report.TransmissionReport.Message.SenderReference,
		report.Distribution.DistributionTag, report.TransmissionReport.Message.NetworkInfo.NetworkPriority, report.Distribution.ID,
		report.Distribution.CloudReference, nowMs, rawText, rawHash)
	if err != nil {
		return fmt.Errorf("upsert messages: %w", err)
	}

	var messageID int64
	err = tx.QueryRow(`SELECT id FROM messages WHERE distribution_id = ?`, report.Distribution.ID).Scan(&messageID)
	if err != nil {
		return fmt.Errorf("query message id: %w", err)
	}

	_, err = tx.Exec(`DELETE FROM mt_data WHERE message_id = ?`, messageID)
	if err != nil {
		return fmt.Errorf("clear mt_data: %w", err)
	}

	stmt, stmtErr := tx.Prepare(`
	INSERT INTO mt_data (message_id, field_tag, field_value, seq_no)
	VALUES (?, ?, ?, ?)
	`)
	if stmtErr != nil {
		return fmt.Errorf("prepare mt_data insert: %w", stmtErr)
	}
	defer stmt.Close()

	for idx, line := range report.TransmissionReport.Message.MT.Line {
		_, execErr := stmt.Exec(messageID, line.Field, line.Data, idx+1)
		if execErr != nil {
			return fmt.Errorf("insert mt_data seq %d: %w", idx+1, execErr)
		}
	}

	err = tx.Commit()
	if err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}

	return nil
}
