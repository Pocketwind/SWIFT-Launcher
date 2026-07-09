package messaging

import (
	"fmt"
	"time"
)

func WriteMXMessageToSQL(message MXDownload, partnerName string) error {
	db, err := getDB()
	if err != nil {
		return fmt.Errorf("getDB: %w", err)
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
INSERT INTO messages 
(partner_name, direction, sender, receiver, message_type, service, possible_duplicate, 
service_code, usage_identifier, sender_reference, tag, priority, distribution_id, 
message_cloud_reference, received_at_ms, raw_text, raw_hash)

VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)

ON CONFLICT(distribution_id) DO UPDATE SET
	partner_name=excluded.partner_name,
	direction=excluded.direction,
	sender=excluded.sender,
	receiver=excluded.receiver,
	message_type=excluded.message_type,
	service=excluded.service,
	possible_duplicate=excluded.possible_duplicate,
	service_code=excluded.service_code,
	usage_identifier=excluded.usage_identifier,
	sender_reference=excluded.sender_reference,
	tag=excluded.tag,
	priority=excluded.priority,
	distribution_id=excluded.distribution_id,
	message_cloud_reference=excluded.message_cloud_reference,
	received_at_ms=excluded.received_at_ms,
	raw_text=excluded.raw_text,
	raw_hash=excluded.raw_hash
`, partnerName, message.Message.Direction, message.Message.Requestor, message.Message.Responder, message.Message.MessageType,
		message.Distribution.Service, message.Distribution.PossibleDuplicate, message.Message.ServiceCode, message.Message.UsageIdentifier,
		message.Message.SenderReference, message.Distribution.DistributionTag, message.Message.NetworkInfo.NetworkPriority,
		message.Distribution.ID, message.Distribution.CloudReference, nowMs, rawText, rawHash)
	if err != nil {
		return fmt.Errorf("insert/update messages: %w", err)
	}

	var messageID int64
	err = tx.QueryRow(`SELECT id FROM messages WHERE distribution_id = ?`, message.Distribution.ID).Scan(&messageID)
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
	db, err := getDB()
	if err != nil {
		return fmt.Errorf("getDB: %w", err)
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
INSERT INTO messages 
(partner_name, direction, sender, receiver, message_type, service, possible_duplicate, 
service_code, usage_identifier, sender_reference, tag, priority, distribution_id, 
message_cloud_reference, received_at_ms, raw_text, raw_hash)

VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)

ON CONFLICT(distribution_id) DO UPDATE SET
	partner_name=excluded.partner_name,
	direction=excluded.direction,
	sender=excluded.sender,
	receiver=excluded.receiver,
	message_type=excluded.message_type,
	service=excluded.service,
	possible_duplicate=excluded.possible_duplicate,
	service_code=excluded.service_code,
	usage_identifier=excluded.usage_identifier,
	sender_reference=excluded.sender_reference,
	tag=excluded.tag,
	priority=excluded.priority,
	distribution_id=excluded.distribution_id,
	message_cloud_reference=excluded.message_cloud_reference,
	received_at_ms=excluded.received_at_ms,
	raw_text=excluded.raw_text,
	raw_hash=excluded.raw_hash
`, partnerName, report.TransmissionReport.Message.Direction, report.TransmissionReport.Message.Requestor, report.TransmissionReport.Message.Responder,
		report.TransmissionReport.Message.MessageType, report.Distribution.Service, report.Distribution.PossibleDuplicate,
		report.TransmissionReport.Message.ServiceCode, report.TransmissionReport.Message.UsageIdentifier,
		report.TransmissionReport.Message.SenderReference, report.Distribution.DistributionTag, report.TransmissionReport.Message.NetworkInfo.NetworkPriority,
		report.Distribution.ID, report.Distribution.CloudReference, nowMs, rawText, rawHash)
	if err != nil {
		return fmt.Errorf("insert/update messages: %w", err)
	}

	var messageID int64
	err = tx.QueryRow(`SELECT id FROM messages WHERE distribution_id = ?`, report.Distribution.ID).Scan(&messageID)
	if err != nil {
		return fmt.Errorf("get message id: %w", err)
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
