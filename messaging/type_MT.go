package messaging

type Field struct {
	Field string `json:"field"`
	Data  string `json:"data"`
}

type MT struct {
	Line []Field `json:"line"`
}
