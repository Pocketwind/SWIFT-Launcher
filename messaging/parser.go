package messaging

import (
	"fmt"
	"strings"

	"github.com/Pocketwind/SWIFT-Launcher/fsutil"
	"github.com/antchfx/xmlquery"
)

func MTParser(payload string) (MT, error) {

	lines := strings.Split(payload, "\n")

	var mt MT
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		field, data, ok := splitTaggedLine(line)
		if !ok {
			//줄바꿈인 경우(:로 시작 안하는거)는 data에 newline 후 추가
			if len(mt.Line) > 0 {
				mt.Line[len(mt.Line)-1].Data += "\n" + line
				continue
			}
			return MT{}, fmt.Errorf("MT payload must start with a tagged field")
		}

		mt.Line = append(mt.Line, Field{
			Field: field,
			Data:  data,
		})
	}
	if len(mt.Line) == 0 {
		return MT{}, fmt.Errorf("MT payload has no fields")
	}

	return mt, nil
}

func splitTaggedLine(line string) (string, string, bool) {
	if !strings.HasPrefix(line, ":") {
		return "", "", false
	}

	parts := strings.SplitN(strings.TrimPrefix(line, ":"), ":", 2)
	if len(parts) != 2 {
		return "", "", false
	}

	field := strings.TrimSpace(parts[0])
	data := strings.TrimSpace(parts[1])
	if field == "" {
		return "", "", false
	}

	return field, data, true
}

func MXParser(payload string) (MX, error) {
	var mx MX

	bodyDoc, err := xmlquery.Parse(strings.NewReader(payload))
	if err != nil {
		return MX{}, fmt.Errorf("error parsing body XML: %w", err)
	}
	appHeader := xmlquery.FindOne(bodyDoc, "//*[local-name()='AppHdr']")
	document := xmlquery.FindOne(bodyDoc, "//*[local-name()='Document']")
	if appHeader == nil || document == nil {
		return MX{}, fmt.Errorf("MX payload requires AppHdr and Document elements")
	}
	mx.AppHeader = appHeader.OutputXML(true)
	mx.Document = document.OutputXML(true)

	mx.AppHeader, err = fsutil.FormatXMLString(mx.AppHeader)
	if err != nil {
		return MX{}, fmt.Errorf("error formatting AppHeader XML: %w", err)
	}

	mx.Document, err = fsutil.FormatXMLString(mx.Document)
	if err != nil {
		return MX{}, fmt.Errorf("error formatting Document XML: %w", err)
	}

	return mx, nil
}
