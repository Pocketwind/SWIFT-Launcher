package messaging

type ReportNetworkInfo struct {
	MessageNetworkPriority   string `json:"message_network_priority,omitempty"`
	MessagePossibleDuplicate bool   `json:"message_possible_duplicate,omitempty"`
	SessionNumber            int    `json:"session_number,omitempty"`
	SequenceNumber           int    `json:"sequence_number,omitempty"`
	RequestSubtype           string `json:"message_request_subtype,omitempty"` //swift.cbprplus.03
	RequestType              string `json:"message_request_type,omitempty"`    //pacs.008.001.08
}

type NetworkInfo struct {
	NetworkPriority       string `json:"network_priority,omitempty"`
	PossibleDuplicate     bool   `json:"possible_duplicate,omitempty"`
	SequenceNumber        int    `json:"sequence_number,omitempty"`
	SessionNumber         int    `json:"session_number,omitempty"`
	MessageInputTime      string `json:"message_input_time,omitempty"`
	MessageInputReference string `json:"message_input_reference,omitempty"`
	LocalOutputTime       string `json:"local_output_time,omitempty"`
	SyntaxVersion         string `json:"syntax_version,omitempty"`
	RequestType           string `json:"request_type,omitempty"`
	TransferDescription   string `json:"transfer_description,omitempty"`
	TransferInfo          string `json:"transfer_info,omitempty"`
	FileDescription       string `json:"file_description,omitempty"`
	FileInfo              string `json:"file_info,omitempty"`
	HeaderInfo            string `json:"header_info,omitempty"`
	ServiceCode           string `json:"service_code,omitempty"`
	Requestor             string `json:"requestor,omitempty"`
	Responder             string `json:"responder,omitempty"`
}
