package messaging

type NetworkInfo struct {
	NetworkPriority       string `json:"network_priority"`
	PossibleDuplicate     bool   `json:"possible_duplicate"`
	SequenceNumber        int    `json:"sequence_number"`
	SessionNumber         int    `json:"session_number"`
	MessageInputTime      string `json:"message_input_time"`
	MessageInputReference string `json:"message_input_reference"`
	LocalOutputTime       string `json:"local_output_time"`
	SyntaxVersion         string `json:"syntax_version"`
}

type ReportNetworkInfo struct {
	MessageNetworkPriority   string `json:"message_network_priority"`
	MessagePossibleDuplicate bool   `json:"message_possible_duplicate"`
	SessionNumber            int    `json:"session_number"`
	SequenceNumber           int    `json:"sequence_number"`
	RequestSubtype           string `json:"message_request_subtype"` //swift.cbprplus.03
	RequestType              string `json:"message_request_type"`    //pacs.008.001.08
}
