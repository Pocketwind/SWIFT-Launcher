package search

import (
	"database/sql"
	"sync"
	"time"
)

type SearchResult struct {
	PartnerName       string `json:"partner_name"`
	Direction         string `json:"direction"`
	Sender            string `json:"sender"`
	Receiver          string `json:"receiver"`
	MessageType       string `json:"message_type"`
	Service           string `json:"service"`
	PossibleDuplicate string `json:"possible_duplicate"`
	ServiceCode       string `json:"service_code"`
	UsageIdentifier   string `json:"usage_identifier"`
	SenderReference   string `json:"sender_reference"`
	Tag               string `json:"tag"`
	Priority          string `json:"priority"`
	DistributionID    int64  `json:"distribution_id"`
	CloudReference    string `json:"cloud_reference"`
	ReceivedAtMs      int64  `json:"received_at_ms"`
	RawText           string `json:"raw_text"`
	RawHash           string `json:"raw_hash"`
}

var (
	messageDBOnce sync.Once
	messageDB     *sql.DB
	messageDBErr  error
	seoulLoc      = loadSeoulLocation()
)

func loadSeoulLocation() *time.Location {
	loc, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		return time.FixedZone("GMT+9", 9*60*60)
	}
	return loc
}

func getMessageDB() (*sql.DB, error) {
	//Read 전용
	messageDBOnce.Do(func() {
		db, err := sql.Open("sqlite", "messages.db")
		if err != nil {
			messageDBErr = err
			return
		}
		if _, err := db.Exec(`PRAGMA busy_timeout = 5000;`); err != nil {
			_ = db.Close()
			messageDBErr = err
			return
		}
		messageDB = db
	})
	return messageDB, messageDBErr
}
