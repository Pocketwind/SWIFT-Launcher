package messaging

// Messaging할때 쓰는 MTData 구조체
type MTData struct {
	SenderReference string `json:"sender_reference"`
	MessageType     string `json:"message_type"` //fin.nnn
	Sender          string `json:"sender"`       //BIC
	Receiver        string `json:"receiver"`     //BIC
	Payload         string `json:"payload"`      //block4 내용
	//options
	//InterfaceInfo InterfaceInfo      `json:"interface_info,omitempty"`
	NetworkInfo NetworkInfo `json:"network_info,omitempty"`
}

// Messaging할때 쓰는 MXData 구조체
type MXData struct {
	SenderReference string `json:"sender_reference"`
	ServiceCode     string `json:"service_code"` //swift.partner.finplus!pc
	MessageType     string `json:"message_type"` //pacs.008.001.02
	Requestor       string `json:"requestor"`    //DN
	Responder       string `json:"responder"`    //DN
	Payload         string `json:"payload"`      //Envelope로 감싼 AppHeader, Body Documents
	//options
	UsageIdentifier string `json:"usage_identifier"` //swift.cbprplus.03
	//InterfaceInfo   InterfaceInfo      `json:"interface_info,omitempty"`
	NetworkInfo  NetworkInfo `json:"network_info,omitempty"`
	SecurityInfo string      `json:"security_info,omitempty"`
	Format       string      `json:"format"` //MX or AnyXML
}

// 메시지 response
type MessageResponse struct {
	Code                  string `json:"code"`
	Severity              string `json:"severity"`
	Text                  string `json:"text"`
	MessageCloudReference string `json:"message_cloud_reference"` //클라우드에서 메시지 검색할때 쓰는 문자열, uetr이랑 다름
}

//다운로드 고루틴 결과 데이터
type downloadResult struct {
	ids []string
	err error
}
type task struct {
	mtype int
	ids   []string
}

const (
	finMsgTask = iota
	finReportTask
	interActMsgTask
	interActReportTask
	fileActMsgTask
	fileActReportTask
)

type InterfaceInfo struct {
	ProductInfo string `json:"product_info,omitempty"`
}

//FA
type FAData struct {
	FileTransferRequest FileTransferRequest `json:"file_transfer_request"`
	CompanionInfo       CompanionInfo       `json:"companion_info"`
}
type FileTransferRequest struct {
	FileAttributes       FileAttributes       `json:"file_attributes"`
	FileOperation        FileOperation        `json:"file_operation"`
	EncryptionAttributes EncryptionAttributes `json:"encryption_attributes"`
}
type CompanionInfo struct {
	SenderReference string `json:"sender_reference"`
	ServiceCode     string `json:"service_code,omitempty"`
	MessageType     string `json:"message_type"`
	Requestor       string `json:"requestor,omitempty"`
	Responder       string `json:"responder,omitempty"`
	FileLogicalName string `json:"file_logical_name"` //클라우드에 등록될 이름
	//Body            string      `json:"body,omitempty"`    //실제 파일이름
	NetworkInfo NetworkInfo `json:"network_info,omitempty"`
}
type FileAttributes struct {
	FileName      string `json:"file_name"`
	FileDigestAlg string `json:"file_digest_alg,omitempty"`
	FileDigest    string `json:"file_digest,omitempty"`
	FileSize      int    `json:"file_size,omitempty"`
}
type FileOperation struct {
	Type string `json:"type"` //upload, download
}
type EncryptionAttributes struct {
	KeyAlg       string `json:"key_alg"`
	KeyValue     string `json:"key_value"`
	KeyDigestAlg string `json:"key_digest_alg"`
	KeyDigest    string `json:"key_digest"`
}
type FileActInitiateResponse struct {
	FileTransferResponse FileTransferResponse `json:"file_transfer_response"`
	TransferID           string               `json:"transfer_id"`
}
type FileTransferResponse struct {
	SignedURLs []SignedURL `json:"signed_urls"`
}
type SignedURL struct {
	Part int    `json:"part"`
	URL  string `json:"url"`
}
