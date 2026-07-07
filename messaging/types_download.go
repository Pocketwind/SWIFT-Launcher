package messaging

import "encoding/xml"

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
	Responder     string            `json:"responder"`                   //DN
	Report        string            `json:"transmission_report_payload"` //ACK 전문
	Message       MXMessage         `json:"message"`                     //ACK에 원본 메시지 들어오는곳
	NetworkInfo   ReportNetworkInfo `json:"network_info"`
	CompanionInfo CompanionInfo     `json:"companion_info,omitempty"` //동일한 메시지에 대한 다른 네트워크의 보고서 정보 (예: MT 메시지에 대한 MX 보고서)
}

// MX 헤더
type MXHeader struct {
	XMLName                    xml.Name `xml:"Message"` //태그 이름
	SenderReference            string   `xml:"SenderReference"`
	MessageIdentifier          string   `xml:"MessageIdentifier"`
	Format                     string   `xml:"Format"`                       //MX, AnyXML
	SubFormat                  string   `xml:"SubFormat"`                    //Input, Output
	SenderDN                   string   `xml:"Sender>DN"`                    //DN
	SenderFullNameX1           string   `xml:"Sender>FullName>X1"`           //BIC11 대문자
	SenderFullNameX2           string   `xml:"Sender>FullName>X2"`           //BIC11 마지막 3글자 소문자
	ReceiverDN                 string   `xml:"Receiver>DN"`                  //DN
	ReceiverFullNameX1         string   `xml:"Receiver>FullName>X1"`         //BIC11 대문자
	ReceiverFullNameX2         string   `xml:"Receiver>FullName>X2"`         //BIC11 마지막 3글자 소문자
	InterfaceInfoCreator       string   `xml:"InterfaceInfo>MessageCreator"` //ApplicationInterface
	InterfaceInfoContext       string   `xml:"InterfaceInfo>MessageContext"` //Report
	InterfaceInfoNature        string   `xml:"InterfaceInfo>MessageNature"`  //Financial
	NetworkInfoPriority        string   `xml:"NetworkInfo>Priority"`
	NetworkInfoDuplicate       bool     `xml:"NetworkInfo>IsPossibleDuplicate"`
	NetworkInfoService         string   `xml:"NetworkInfo>Service"`                            //swift.partner.finplus!pc
	NetworkInfoSWIFTNetType    string   `xml:"NetworkInfo>SWIFTNetNetworkInfo>RequestType"`    //pacs.008.001.08
	NetworkInfoSWIFTNetSubtype string   `xml:"NetworkInfo>SWIFTNetNetworkInfo>RequestSubType"` //swift.cbprplus.03
	//ExpiryDateTime string  `xml:"NetworkInfo>ExpiryDateTime"`
}
type MXReportHeader struct {
	XMLName                     xml.Name `xml:"TransmissionReport"`
	SenderReference             string   `xml:"SenderReference"`
	NetworkDeliveryStatus       string   `xml:"NetworkDeliveryStatus"`        //NetworkNacked?
	OriginalInstanceAddresseeX1 string   `xml:"OriginalInstanceAddressee>X1"` //원본 메시지 ReceiverDN X1
	OriginalInstanceAddresseeX2 string   `xml:"OriginalInstanceAddressee>X2"` //원본 메시지 ReceiverDN X2
	ReportingApplication        string   `xml:"ReportingApplication"`         //SWIFTNetInterface
	Priority                    string   `xml:"NetworkInfo>Priority"`
	IsPossibleDuplicate         bool     `xml:"NetworkInfo>IsPossibleDuplicate"`
	Service                     string   `xml:"NetworkInfo>Service"` //swift.partner.finplus!pc
	Network                     string   `xml:"NetworkInfo>Network"` //SWIFTNet
	SessionNr                   int      `xml:"NetworkInfo>SessionNr"`
	SeqNr                       int      `xml:"NetworkInfo>SeqNr"`
	RequestType                 string   `xml:"NetworkInfo>SWIFTNetNetworkInfo>RequestType"`    //pacs.008.001.08
	RequestSubtype              string   `xml:"NetworkInfo>SWIFTNetNetworkInfo>RequestSubType"` //swift.cbprplus.03
	IntvCategory                string   `xml:"Interventions>Intervention>IntvCategory"`
	CreationTime                string   `xml:"Interventions>Intervention>CreationTime"`
	OperatorOrigin              string   `xml:"Interventions>Intervention>OperatorOrigin"`
	Contents                    string   `xml:"Interventions>Intervention>Contents"` //ACK 전문 내용
	IsRelatedInstanceOriginal   bool     `xml:"IsRelatedInstanceOriginal"`
	MessageCreator              string   `xml:"MessageCreator"` //ApplicationInterface
	IsMessageModified           bool     `xml:"IsMessageModified"`
	MessageFields               string   `xml:"MessageFields"` //HeaderAndBody
	Message                     MXHeader `xml:"Message"`
}

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
	MT              MT          `json:"mt"`        //MT 구조
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
type FileActReport struct {
	Distribution       Distribution         `json:"distribution"`
	TransmissionReport MXTransmissionReport `json:"transmission_report"`
}
