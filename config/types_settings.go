package config

import "net/http"

type Settings struct {
	//User-Agent 관련 설정
	AppName      string `json:"appName"`
	AppVersion   string `json:"appVersion"`
	PartnerBIC   string `json:"partnerBIC"`
	CustomerType string `json:"customerType"` //PIC, BIC, LEI, VAT, HASH
	CACertPath   string `json:"cacertPath"`
	//messaging 설정
	Messaging Messaging `json:"messaging"`
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
	FileActAckUrl       string `json:"fileActAckUrl"`
	FileActReportUrl    string `json:"fileActReportUrl"`
	Proxy               string `json:"proxy"`
	PartnerFilePath     string `json:"partnerFilePath"`
	DownloadPath        string `json:"downloadPath"`
	MaxDistributionSize int    `json:"maxDistributionSize"`
	UpdateInterval      int    `json:"updateInterval"`
	RequestTimeout      int    `json:"requestTimeout"`
	HttpClient          *http.Client
}
type Partner struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	Type         string `json:"type"`
	InputPath    string `json:"input_path"`
	OutputPath   string `json:"output_path"`
	AckPath      string `json:"ack_path"`
	ErrorPath    string `json:"error_path"`
	Extension    string `json:"extension"`
	InputChannel chan string
}
