package messaging

type Field struct {
	Field string `json:"field"`
	Data  string `json:"data"`
}

type MT struct {
	Line []Field `json:"line"`
}

type MX struct {
	AppHeader string `json:"app_header"`
	Document  string `json:"document"`
}
