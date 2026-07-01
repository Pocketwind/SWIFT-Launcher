package config

import (
	"net/http"
)

type Settings struct {
	//User-Agent 관련 설정
	AppName      string `json:"appName"`
	AppVersion   string `json:"appVersion"`
	PartnerBIC   string `json:"partnerBIC"`
	CustomerType string `json:"customerType"` //PIC, BIC, LEI, VAT, HASH
	CACertPath   string `json:"cacertPath"`
	//messaging 설정
	Messaging Messaging `json:"messaging"`
	//status 설정
	Status Status `json:"status"`
}

// Status 서비스 설정 (localhost 전용)
type Status struct {
	Port int `json:"port"` // 0이면 비활성화
}
type Messaging struct {
	ConsumerKey         string `json:"consumerKey"`
	ConsumerSecret      string `json:"consumerSecret"`
	Audience            string `json:"audience"`
	Subject             string `json:"subject"`
	GrantType           string `json:"grantType"`
	Scope               string `json:"scope"`
	PubKeyPath          string `json:"pubKeyPath"`
	PrivKeyPath         string `json:"privKeyPath"`
	TokenUrl            string `json:"tokenUrl"`
	RevokeTokenUrl      string `json:"revokeTokenUrl"`
	DistributionUrl     string `json:"distUrl"`
	FinReportUrl        string `json:"finReportUrl"`
	FinMessageUrl       string `json:"finMessageUrl"`
	InterActReportUrl   string `json:"interActReportUrl"`
	InterActMessageUrl  string `json:"interActMessageUrl"`
	AckUrl              string `json:"ackUrl"`
	FileActUrl          string `json:"fileActUrl"`
	FileActMessageUrl   string `json:"fileActMessageUrl"`
	FileActAckUrl       string `json:"fileActAckUrl"`
	FileActReportUrl    string `json:"fileActReportUrl"`
	Proxy               string `json:"proxy"`
	PartnerFilePath     string `json:"partnerFilePath"`
	DownloadPath        string `json:"downloadPath"`
	MaxDistributionSize int    `json:"maxDistributionSize"`
	UpdateInterval      int    `json:"updateInterval"`
	RequestTimeout      int    `json:"requestTimeout"`
	TokenTimeout        int    `json:"tokenTimeout"`
	HttpClient          *http.Client
}

// 파트너
type Partner struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	Status       bool   `json:"status"`    //true면 켜진거
	Direction    string `json:"direction"` //in, out
	Type         string `json:"type"`
	InputPath    string `json:"input_path"`
	OutputPath   string `json:"output_path"`
	AckPath      string `json:"ack_path"`
	ErrorPath    string `json:"error_path"`
	ProgressPath string `json:"progress_path"`
	Extension    string `json:"extension"`
	Route        Route  `json:"route"`
	InputChannel chan string
	DFAInfo      DFAInfo `json:"network_info,omitempty"`
	IsDFA        bool    `json:"is_dfa"`
}
type Route struct {
	Sender      string `json:"sender"`
	Receiver    string `json:"receiver"`
	MessageType string `json:"message_type"`
}
type DFAInfo struct {
	RequestType         string `json:"request_type,omitempty"`
	TransferDescription string `json:"transfer_description,omitempty"`
	TransferInfo        string `json:"transfer_info,omitempty"`
	FileDescription     string `json:"file_description,omitempty"`
	FileInfo            string `json:"file_info,omitempty"`
	HeaderInfo          string `json:"header_info,omitempty"`
	ServiceCode         string `json:"service_code,omitempty"`
	Requestor           string `json:"requestor,omitempty"`
	Responder           string `json:"responder,omitempty"`
}

type StatusData struct {
	Name         string  `json:"name"`
	Direction    string  `json:"direction"`
	Status       bool    `json:"status"`
	Type         string  `json:"type"`
	InputPath    string  `json:"input_path,omitempty"`
	OutputPath   string  `json:"output_path,omitempty"`
	AckPath      string  `json:"ack_path,omitempty"`
	ErrorPath    string  `json:"error_path,omitempty"`
	ProgressPath string  `json:"progress_path,omitempty"`
	Extension    string  `json:"extension,omitempty"`
	IsDFA        bool    `json:"is_dfa,omitempty"`
	DFAInfo      DFAInfo `json:"network_info,omitempty"`
	Route        Route   `json:"route,omitempty"`
}
