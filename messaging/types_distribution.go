package messaging

// Distributions 구조체
type Distribution struct {
	ID                   int                  `json:"id"`                               //클라우드 안에서 구분하는 번호, 전문마다 각각 다름(Ack, I/O 다 다름)
	Service              string               `json:"service"`                          //interAct, fin, fileAct
	Type                 string               `json:"type"`                             //message, transmissionReport
	DistributionTag      string               `json:"distribution_tag"`                 //클라우드에서 Distribute할떄 쓰는 태그, 없으면 필드 없음
	PossibleDuplicate    bool                 `json:"possible_duplication"`             //클라우드에서 한번이라도 다운로드 요청하면 ture로 바뀜
	CloudReference       string               `json:"message_cloud_reference"`          //클라우드에서 메시지 검색할때 쓰는 문자열, uetr이랑 다름
	FileTransferResponse FileTransferResponse `json:"file_transfer_response,omitempty"` //FileAct 메시지인 경우에만 존재하는 필드
	CompanionInfo        CompanionInfo        `json:"companion_info,omitempty"`         //FileAct 메시지인 경우에만 존재하는 필드
}
type Distributions struct {
	Distributions []Distribution `json:"distributions"`
}
