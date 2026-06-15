package messaging

// Download struct
type MXDownload struct {
	Distribution Distribution `json:"distribution"`
	Message      MXMessage    `json:"message"`
}
type MXMessage struct {
	SenderReference string      `json:"sender_reference"`
	ServiceCode     string      `json:"service_code"`     //swift.partner.finplus!pc
	MessageType     string      `json:"message_type"`     //pacs.008.001.02
	Direction       string      `json:"direction"`        //Incoming, Outgoing
	Requestor       string      `json:"requestor"`        //DN
	Responder       string      `json:"responder"`        //DN
	UsageIdentifier string      `json:"usage_identifier"` //swift.cbprplus.03
	NetworkInfo     NetworkInfo `json:"network_info"`     //MT랑 약간 달라서 확인해봐야함
	Format          string      `json:"format"`           //MX or AnyXML
	Payload         string      `json:"payload"`          //Envelope로 감싼 AppHeader, Body Documents
}
type MXReport struct { //Report 다운로드한거
	Distribution       Distribution         `json:"distribution"`
	TransmissionReport MXTransmissionReport `json:"transmission_report"`
}
type MXTransmissionReport struct { //transmission_report
	SenderReference string `json:"sender_reference"`
	ServiceCode     string `json:"service_code"`    //swift.partner.finplus!pc
	SwiftReference  string `json:"swift_reference"` //swi03002-2026-04-06T02:13:54.28840.004998Z 형태로 GMT+0 서버 처리 시간인듯?
	ResponseDate    string `json:"response_date"`   //ACK처리시간 2026-04-06T02:13:53Z
	DeliveryStatus  string `json:"delivery_status"` //Acked, Nacked
	//RejectionCode   string    `json:"rejection_code"`
	Responder   string            `json:"responder"`                   //DN
	Report      string            `json:"transmission_report_payload"` //ACK 전문
	Message     MXMessage         `json:"message"`                     //ACK에 원본 메시지 들어오는곳
	NetworkInfo ReportNetworkInfo `json:"network_info"`
}
