package messaging

import (
	"encoding/base64"
	"fmt"
	"strings"
)

func MTParser(payload string) (MT, error) {
	//base64 decode
	payloadDecoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return MT{}, fmt.Errorf("error decoding payload: %w", err)
	}

	lines := strings.Split(string(payloadDecoded), "\n")

	var mt MT
	for idx, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		field, data, ok := splitTaggedLine(line)
		if !ok {
			return MT{}, fmt.Errorf("invalid MT field format at line %d: %s", idx+1, line)
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
