package messaging

type MTDownload struct {
	Distribution Distribution `json:"distribution"`
	Message      MTMessage    `json:"message"`
}
type MTMessage struct {
	SenderReference string      `json:"sender_reference"`
	MessageType     string      `json:"message_type"` //fin.nnn
	Sender          string      `json:"sender"`       //BIC
	Receiver        string      `json:"receiver"`     //BIC
	NetworkInfo     NetworkInfo `json:"network_info"`
	Payload         string      `json:"payload"`   //block4 내용
	Direction       string      `json:"direction"` //Incoming, Outgoing
}
type MTTransmissionReport struct { //transmission_report
	//MT
	SenderReference string            `json:"sender_reference"`
	ResponseDate    string            `json:"response_date"`    //Ack처리시간 2026-04-06T02:13:53Z
	DeliveryStatus  string            `json:"delivery_status"`  //Acked, Nacked
	RejectionCode   string            `json:"rejection_code"`   //VAL 이런식으로 나옴
	RejectionReason string            `json:"rejection_reason"` //Access처럼 숫자코드가 아니라 MX처럼 상세하게 나옴
	Receiver        string            `json:"receiver"`         //BIC
	NetworkInfo     ReportNetworkInfo `json:"network_info"`
	Message         MTMessage         `json:"message"` //원본 전문
}
type MTReport struct { //Report 다운로드한거
	Distribution       Distribution         `json:"distribution"`
	TransmissionReport MTTransmissionReport `json:"transmission_report"`
}
