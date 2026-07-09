package search

import (
	"database/sql"
	"flag"
	"fmt"
	"io"
	"time"
)

type SearchOptions struct {
	Partner           string
	Direction         string
	Sender            string
	Receiver          string
	Type              string
	Service           string
	PossibleDuplicate string
	ServiceCode       string
	UsageIdentifier   string
	SenderReference   string
	Tag               string
	Priority          string
	DistributionID    int64
	CloudReference    string
	Start             string
	End               string
	Limit             int
	Path              string
	Terms             []string
}

func ParseSearchArgs(args []string) (SearchOptions, error) {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	partner := fs.String("partner", "", "Partner name")
	direction := fs.String("direction", "", "Message direction (Incoming or Outgoing)")
	sender := fs.String("sender", "", "Sender")
	receiver := fs.String("receiver", "", "Receiver")
	mtype := fs.String("type", "", "Message type")
	service := fs.String("service", "", "MX or MT")
	possibleDuplicate := fs.String("possible_duplicate", "", "Possible duplicate")
	serviceCode := fs.String("service_code", "", "Service code")
	usageIdentifier := fs.String("usage_identifier", "", "Usage identifier")
	senderReference := fs.String("sender_reference", "", "Sender reference")
	tag := fs.String("tag", "", "Tag")
	priority := fs.String("priority", "", "Priority")
	distributionID := fs.Int64("distribution_id", 0, "Distribution ID")
	cloudReference := fs.String("cloud_reference", "", "Cloud reference")
	start := fs.String("start", "", "Start date (YYYY-MM-DDTHH:MM:SS)")
	end := fs.String("end", "", "End date (YYYY-MM-DDTHH:MM:SS)")
	limit := fs.Int("limit", 100, "Limit number of results")
	path := fs.String("path", "", "Export directory")

	if err := fs.Parse(args); err != nil {
		return SearchOptions{}, err
	}

	terms := fs.Args()

	return SearchOptions{
		Partner:           *partner,
		Direction:         *direction,
		Sender:            *sender,
		Receiver:          *receiver,
		Type:              *mtype,
		Service:           *service,
		PossibleDuplicate: *possibleDuplicate,
		ServiceCode:       *serviceCode,
		UsageIdentifier:   *usageIdentifier,
		SenderReference:   *senderReference,
		Tag:               *tag,
		Priority:          *priority,
		DistributionID:    *distributionID,
		CloudReference:    *cloudReference,
		Start:             *start,
		End:               *end,
		Limit:             *limit,
		Path:              *path,
		Terms:             terms,
	}, nil
}

func SearchMessages(args []string) error {
	options, err := ParseSearchArgs(args)
	if err != nil {
		return err
	}

	//DB 연결
	db, err := getMessageDB()
	if err != nil {
		println("Error opening database:", err.Error())
		return err
	}

	//tx
	tx, err := db.Begin()
	if err != nil {
		println("Error starting transaction:", err.Error())
		return err
	}
	defer tx.Rollback()

	//검색
	results, err := searchMessages(tx, options)
	if err != nil {
		println("Error searching messages:", err.Error())
		return err
	}

	//출력
	printMessages(results)

	return nil
}
func printMessages(results []SearchResult) {
	fmt.Println("--------------------------------------------------")
	if len(results) == 0 {
		fmt.Println("No messages found.")
		return
	} else {
		fmt.Printf("Found %d messages:\n", len(results))
	}
	fmt.Println("--------------------------------------------------")

	for _, msg := range results {
		printMessage(msg)
		fmt.Println("--------------------------------------------------")
	}
}

func printMessage(msg SearchResult) {
	fmt.Printf("Partner: %s\n", msg.PartnerName)
	fmt.Printf("Direction: %s\n", msg.Direction)
	fmt.Printf("Sender: %s\n", msg.Sender)
	fmt.Printf("Receiver: %s\n", msg.Receiver)
	fmt.Printf("Message Type: %s\n", msg.MessageType)
	fmt.Printf("Service: %s\n", msg.Service)
	fmt.Printf("Sender Reference: %s\n", msg.SenderReference)
	fmt.Printf("Cloud Reference: %s\n", msg.CloudReference)
	fmt.Printf("Processed Time: %s\n", MstoDate(msg.ReceivedAtMs))
}

func searchMessages(tx *sql.Tx, options SearchOptions) ([]SearchResult, error) {
	//검색어
	partner := options.Partner
	direction := options.Direction
	sender := options.Sender
	receiver := options.Receiver
	mtype := options.Type
	service := options.Service
	possibleDuplicate := options.PossibleDuplicate
	serviceCode := options.ServiceCode
	usageIdentifier := options.UsageIdentifier
	senderReference := options.SenderReference
	tag := options.Tag
	priority := options.Priority
	distributionID := options.DistributionID
	cloudReference := options.CloudReference
	start := options.Start
	end := options.End
	limit := options.Limit

	//시간 변환
	var msStart, msEnd int64
	var err error
	if start != "" {
		msStart, err = DateToMs(start)
		if err != nil {
			return nil, fmt.Errorf("invalid start date: %w", err)
		}
	}
	if end != "" {
		msEnd, err = DateToMs(end)
		if err != nil {
			return nil, fmt.Errorf("invalid end date: %w", err)
		}
	}

	//쿼리
	query := `SELECT
		partner_name,
		direction,
		sender,
		receiver,
		message_type,
		service,
		COALESCE(possible_duplicate, ''),
		COALESCE(service_code, ''),
		COALESCE(usage_identifier, ''),
		sender_reference,
		COALESCE(tag, ''),
		priority,
		distribution_id,
		message_cloud_reference,
		received_at_ms,
		raw_text,
		raw_hash
	FROM messages WHERE 1=1`
	args := []interface{}{}
	//옵션
	if partner != "" {
		query += " AND partner_name LIKE ?"
		args = append(args, "%"+partner+"%")
	}
	if direction != "" {
		query += " AND direction = ?"
		args = append(args, direction)
	}
	if sender != "" {
		query += " AND sender LIKE ?"
		args = append(args, "%"+sender+"%")
	}
	if receiver != "" {
		query += " AND receiver LIKE ?"
		args = append(args, "%"+receiver+"%")
	}
	if mtype != "" {
		query += " AND message_type LIKE ?"
		args = append(args, "%"+mtype+"%")
	}
	if service != "" {
		query += " AND service = ?"
		args = append(args, service)
	}
	if possibleDuplicate != "" {
		query += " AND possible_duplicate = ?"
		args = append(args, possibleDuplicate)
	}
	if serviceCode != "" {
		query += " AND service_code = ?"
		args = append(args, serviceCode)
	}
	if usageIdentifier != "" {
		query += " AND usage_identifier = ?"
		args = append(args, usageIdentifier)
	}
	if senderReference != "" {
		query += " AND sender_reference = ?"
		args = append(args, senderReference)
	}
	if tag != "" {
		query += " AND tag = ?"
		args = append(args, tag)
	}
	if priority != "" {
		query += " AND priority = ?"
		args = append(args, priority)
	}
	if distributionID != 0 {
		query += " AND distribution_id = ?"
		args = append(args, distributionID)
	}
	if cloudReference != "" {
		query += " AND message_cloud_reference = ?"
		args = append(args, cloudReference)
	}
	if start != "" {
		query += " AND received_at_ms >= ?"
		args = append(args, msStart)
	}
	if end != "" {
		query += " AND received_at_ms <= ?"
		args = append(args, msEnd)
	}
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := tx.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var msg SearchResult
		err := rows.Scan(
			&msg.PartnerName,
			&msg.Direction,
			&msg.Sender,
			&msg.Receiver,
			&msg.MessageType,
			&msg.Service,
			&msg.PossibleDuplicate,
			&msg.ServiceCode,
			&msg.UsageIdentifier,
			&msg.SenderReference,
			&msg.Tag,
			&msg.Priority,
			&msg.DistributionID,
			&msg.CloudReference,
			&msg.ReceivedAtMs,
			&msg.RawText,
			&msg.RawHash,
		)
		if err != nil {
			return nil, err
		}
		results = append(results, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return results, nil
}

func MstoDate(ms int64) string {
	t := time.UnixMilli(ms).In(seoulLoc)
	return t.Format("2006-01-02T15:04:05.000")
}
func DateToMs(dateStr string) (int64, error) {
	if dateStr == "" {
		return 0, fmt.Errorf("empty datetime")
	}

	// Inputs with timezone offset are parsed as-is.
	timezoneAwareLayouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
	}
	for _, layout := range timezoneAwareLayouts {
		if t, err := time.Parse(layout, dateStr); err == nil {
			return t.UnixMilli(), nil
		}
	}

	// Inputs without timezone are interpreted in GMT+9 (Asia/Seoul).
	localLayouts := []string{
		"2006-01-02T15:04:05.000",
		"2006-01-02T15:04:05",
		"2006-01-02T15:04",
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006-01-02",
		"20060102",
	}

	for _, layout := range localLayouts {
		if t, err := time.ParseInLocation(layout, dateStr, seoulLoc); err == nil {
			return t.UnixMilli(), nil
		}
	}

	return 0, fmt.Errorf("unsupported datetime format")
}
