package search

type MTSearchResult struct {
	PartnerName     string
	Direction       string
	DistributionID  int64
	Sender          string
	Receiver        string
	Type            string
	SenderReference string
	Priority        string
	ReceivedAtMs    int64
	RawText         string
	RawHash         string
	ParseOK         int
}

type MXSearchResult struct {
	PartnerName     string
	Direction       string
	DistributionID  int64
	Requestor       string
	Responder       string
	Type            string
	SenderReference string
	Priority        string
	ReceivedAtMs    int64
	RawText         string
	RawHash         string
}
