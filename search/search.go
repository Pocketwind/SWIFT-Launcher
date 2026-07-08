package search

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"sync"
	"time"
)

var (
	messageDBOnce sync.Once
	messageDB     *sql.DB
	messageDBErr  error
)

func getMessageDB() (*sql.DB, error) {
	//Read 전용
	messageDBOnce.Do(func() {
		db, err := sql.Open("sqlite", "messages.db")
		if err != nil {
			messageDBErr = err
			return
		}
		messageDB = db
	})
	return messageDB, messageDBErr
}

type SearchOptions struct {
	Direction string
	Format    string
	Type      string
	Requestor string
	Responder string
	Sender    string
	Receiver  string
	Partner   string
	Start     string
	End       string
	Limit     int
	Terms     []string
}

func ParseSearchArgs(args []string) (SearchOptions, error) {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	direction := fs.String("direction", "", "Message direction (Incoming or Outgoing)")
	format := fs.String("format", "", "Message format (MT or MX)")
	mtype := fs.String("type", "", "Message type")
	requestor := fs.String("requestor", "", "Requestor")
	responder := fs.String("responder", "", "Responder")
	sender := fs.String("sender", "", "Sender")
	receiver := fs.String("receiver", "", "Receiver")
	partner := fs.String("partner", "", "Partner name")
	start := fs.String("start", "", "Start date (YYYY-MM-DDTHH:MM:SS)")
	end := fs.String("end", "", "End date (YYYY-MM-DDTHH:MM:SS)")
	limit := fs.Int("limit", 100, "Limit number of results")

	if err := fs.Parse(args); err != nil {
		return SearchOptions{}, err
	}

	terms := fs.Args()

	return SearchOptions{
		Direction: *direction,
		Format:    *format,
		Type:      *mtype,
		Requestor: *requestor,
		Responder: *responder,
		Sender:    *sender,
		Receiver:  *receiver,
		Partner:   *partner,
		Start:     *start,
		End:       *end,
		Limit:     *limit,
		Terms:     terms,
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
	switch options.Format {
	case "MT":
		// MT 검색
		mtResults, err := searchMTMessages(tx, options)
		if err != nil {
			println("Error searching MT messages:", err.Error())
			return err
		}
		// 결과 처리 로직 추가
		fmt.Printf("Found %d MT messages\n", len(mtResults))
		printMTResults(mtResults)
	case "MX":
		// MX 검색
		mxResults, err := searchMXMessages(tx, options)
		if err != nil {
			println("Error searching MX messages:", err.Error())
			return err
		}
		// 결과 처리 로직 추가
		fmt.Printf("Found %d MX messages\n", len(mxResults))
		printMXResults(mxResults)
	default:
		println("Unknown message format:", options.Format)
		return errors.New("MX or MT format must be specified")
	}

	return nil
}

func printMTResults(results []MTSearchResult) {
	for _, msg := range results {
		fmt.Printf("-------------------------------------------\n")
		fmt.Printf("%v\n", msg)
	}
}

func printMXResults(results []MXSearchResult) {
	for _, msg := range results {
		fmt.Printf("-------------------------------------------\n")
		fmt.Printf("%v\n", msg)
	}
}

func searchMXMessages(tx *sql.Tx, options SearchOptions) ([]MXSearchResult, error) {
	partner := options.Partner
	requestor := options.Requestor
	responder := options.Responder
	mtype := options.Type
	direction := options.Direction
	start := options.Start
	end := options.End
	limit := options.Limit

	//시간 변환
	startDate, _ := time.Parse("2006-01-02T15:04:05", start)
	endDate, _ := time.Parse("2006-01-02T15:04:05", end)
	msStart := startDate.UnixMilli()
	msEnd := endDate.UnixMilli()

	query := `SELECT partner_name, direction, distribution_id, requestor, responder, type, sender_reference, priority, received_at_ms, raw_text, raw_hash FROM mx_messages WHERE 1=1`
	args := []interface{}{}

	if partner != "" {
		query += " AND partner_name LIKE ?"
		args = append(args, "%"+partner+"%")
	}
	if requestor != "" {
		query += " AND requestor LIKE ?"
		args = append(args, "%"+requestor+"%")
	}
	if responder != "" {
		query += " AND responder LIKE ?"
		args = append(args, "%"+responder+"%")
	}
	if direction != "" {
		query += " AND direction = ?"
		args = append(args, direction)
	}
	if mtype != "" {
		query += " AND type LIKE ?"
		args = append(args, "%"+mtype+"%")
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

	var results []MXSearchResult
	for rows.Next() {
		var msg MXSearchResult
		err := rows.Scan(&msg.PartnerName, &msg.Direction, &msg.DistributionID, &msg.Requestor, &msg.Responder, &msg.Type, &msg.SenderReference, &msg.Priority, &msg.ReceivedAtMs, &msg.RawText, &msg.RawHash)
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

func searchMTMessages(tx *sql.Tx, options SearchOptions) ([]MTSearchResult, error) {
	partner := options.Partner
	sender := options.Sender
	receiver := options.Receiver
	mtype := options.Type
	direction := options.Direction
	start := options.Start
	end := options.End
	limit := options.Limit

	//시간 변환
	startDate, _ := time.Parse("2006-01-02T15:04:05", start)
	endDate, _ := time.Parse("2006-01-02T15:04:05", end)
	msStart := startDate.UnixMilli()
	msEnd := endDate.UnixMilli()

	query := `SELECT partner_name, direction, distribution_id, sender, receiver, type, sender_reference, priority, received_at_ms, raw_text, raw_hash, parse_ok FROM mt_messages WHERE 1=1`
	args := []interface{}{}

	if partner != "" {
		query += " AND partner_name LIKE ?"
		args = append(args, "%"+partner+"%")
	}
	if sender != "" {
		query += " AND sender LIKE ?"
		args = append(args, "%"+sender+"%")
	}
	if receiver != "" {
		query += " AND receiver LIKE ?"
		args = append(args, "%"+receiver+"%")
	}
	if direction != "" {
		query += " AND direction = ?"
		args = append(args, direction)
	}
	if mtype != "" {
		query += " AND type LIKE ?"
		args = append(args, "%"+mtype+"%")
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

	var results []MTSearchResult
	for rows.Next() {
		var msg MTSearchResult
		err := rows.Scan(&msg.PartnerName, &msg.Direction, &msg.DistributionID, &msg.Sender, &msg.Receiver, &msg.Type, &msg.SenderReference, &msg.Priority, &msg.ReceivedAtMs, &msg.RawText, &msg.RawHash, &msg.ParseOK)
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
