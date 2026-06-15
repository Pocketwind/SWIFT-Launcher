package useragent

// User-Agent 구조체
type CustomerIdentifier struct {
	Type  string //"BIC", "LEI", "VAT", "HASH"
	Value string
}
type UserAgentConfig struct {
	AppName            string
	AppVersion         string
	PartnerBIC         string
	CustomerIdentifier CustomerIdentifier
}
