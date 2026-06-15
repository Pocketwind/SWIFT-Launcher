package messaging

// Messaging할때 쓰는 MTData 구조체
type MTData struct {
	SenderReference string
	MessageType     string //fin.nnn
	Sender          string //BIC
	Receiver        string //BIC
	Payload         string //block4 내용
	//options
	InterfaceInfo string
	NetworkInfo   string
}

// Messaging할때 쓰는 MXData 구조체
type MXData struct {
	SenderReference string
	ServiceCode     string //swift.partner.finplus!pc
	MessageType     string //pacs.008.001.02
	Requestor       string //DN
	Responder       string //DN
	Payload         string //Envelope로 감싼 AppHeader, Body Documents
	//options
	UsageIdentifier string //swift.cbprplus.03
	InterfaceInfo   string
	NetworkInfo     string
	SecurityInfo    string
	Format          string //MX or AnyXML
}

// 메시지 response
type MessageResponse struct {
	Code                  string `json:"code"`
	Severity              string `json:"severity"`
	Text                  string `json:"text"`
	MessageCloudReference string `json:"message_cloud_reference"` //클라우드에서 메시지 검색할때 쓰는 문자열, uetr이랑 다름
}
