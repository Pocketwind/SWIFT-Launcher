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
		}

		mt.Line = append(mt.Line, Field{
			Field: field,
			Data:  data,
		})
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
	mx.AppHeader = xmlquery.FindOne(bodyDoc, "//*[local-name()='AppHdr']").OutputXML(true)
	mx.Document = xmlquery.FindOne(bodyDoc, "//*[local-name()='Document']").OutputXML(true)

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
