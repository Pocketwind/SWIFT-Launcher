package validation

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var mtTag = regexp.MustCompile(`^:([0-9]{2}[A-Z]?):(.*)$`)

type mtField struct {
	tag   string
	lines []string
	line  int
}

// ValidateMT checks reference codes in FIN headers and typed fields. Free-form
// names, addresses, remittance text, and domestic clearing IDs are not codes.
// This is reference validation, not a complete Standards MT rule validator.
func (v *Validator) ValidateMT(message string) error {
	basic, err := mtBlock(message, "1")
	if err != nil {
		return err
	}
	application, err := mtBlock(message, "2")
	if err != nil {
		return err
	}
	if len(basic) < 15 || !strings.HasPrefix(basic, "F01") || len(application) < 4 {
		return fmt.Errorf("MT message has invalid FIN headers")
	}

	var errs []error
	checkLT := func(address, location string) {
		if len(address) != 12 {
			errs = append(errs, fmt.Errorf("%s: invalid logical terminal address %q", location, address))
			return
		}
		if address[8] < 'A' || address[8] > 'Z' {
			errs = append(errs, fmt.Errorf("%s: invalid logical terminal identifier in %q", location, address))
			return
		}
		// A FIN LT address inserts a logical-terminal character after BIC8.
		errs = append(errs, v.checkBIC(address[:8]+address[9:], location))
	}
	switch application[0] {
	case 'I':
		if len(application) < 16 {
			return fmt.Errorf("MT message has truncated input application header")
		}
		checkLT(basic[3:15], "MT Sender")
		checkLT(application[4:16], "MT Receiver")
	case 'O':
		if len(application) < 26 {
			return fmt.Errorf("MT message has truncated output application header")
		}
		checkLT(application[14:26], "MT Sender")
		checkLT(basic[3:15], "MT Receiver")
	default:
		return fmt.Errorf("MT message has unsupported application header %q", application[:1])
	}

	start := strings.Index(message, "{4:")
	if start < 0 {
		return fmt.Errorf("MT message is missing block 4")
	}
	start += len("{4:")
	end := strings.Index(message[start:], "-}")
	if end < 0 {
		return fmt.Errorf("MT message has unterminated block 4")
	}
	fields, err := mtFields(message[start : start+end])
	if err != nil {
		return err
	}
	for _, field := range fields {
		errs = append(errs, v.validateMTField(field, application[1:4]))
	}
	return errors.Join(errs...)
}

func mtBlock(message, number string) (string, error) {
	marker := "{" + number + ":"
	start := strings.Index(message, marker)
	if start < 0 {
		return "", fmt.Errorf("MT message is missing block %s", number)
	}
	start += len(marker)
	end := strings.IndexByte(message[start:], '}')
	if end < 0 {
		return "", fmt.Errorf("MT message has unterminated block %s", number)
	}
	return message[start : start+end], nil
}

func mtFields(body string) ([]mtField, error) {
	var fields []mtField
	for index, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if match := mtTag.FindStringSubmatch(line); match != nil {
			fields = append(fields, mtField{tag: match[1], lines: []string{match[2]}, line: index + 1})
		} else if len(fields) != 0 {
			fields[len(fields)-1].lines = append(fields[len(fields)-1].lines, line)
		} else {
			return nil, fmt.Errorf("MT block 4 line %d must start with a tagged field", index+1)
		}
	}
	if len(fields) == 0 {
		return nil, fmt.Errorf("MT block 4 has no fields")
	}
	return fields, nil
}

func (v *Validator) validateMTField(field mtField, messageType string) error {
	location := fmt.Sprintf("MT :%s: (block 4 line %d)", field.tag, field.line)
	first := field.lines[0]
	currencyAt := func(value string, offset int) error {
		if len(value) < offset+3 {
			return fmt.Errorf("%s: missing or truncated currency code", location)
		}
		return v.checkCurrency(value[offset:offset+3], location)
	}
	qualified := func() (string, error) {
		_, value, ok := strings.Cut(first, "//")
		if !ok {
			return "", fmt.Errorf("%s: missing qualified value separator //", location)
		}
		return value, nil
	}
	switch field.tag {
	case "50A", "51A", "52A", "53A", "54A", "55A", "56A", "57A", "58A", "59A", "82A", "83A", "84A", "85A", "86A", "87A", "88A", "89A":
		return v.checkBIC(field.lines[len(field.lines)-1], location)
	case "50C":
		// In MT 101 this option identifies the instructing party by BIC.
		if messageType == "101" {
			return v.checkBIC(field.lines[len(field.lines)-1], location)
		}
	case "95P":
		value, err := qualified()
		if err != nil {
			return err
		}
		return v.checkBIC(value, location)
	case "32A", "33A", "34A":
		return currencyAt(first, 6)
	case "32B", "33B", "34B", "34F", "71F", "71G":
		return currencyAt(first, 0)
	case "60F", "60M", "62F", "62M", "64", "65":
		return currencyAt(first, 7) // debit/credit mark, date, then currency
	case "11A", "19A", "19B", "90B", "90F", "92B", "92F", "92H", "92M":
		value, err := qualified()
		if err != nil {
			return err
		}
		if field.tag == "19A" && len(value) >= 4 && value[0] == 'N' && value[3] >= 'A' && value[3] <= 'Z' {
			// Four leading letters indicate the optional sign plus currency;
			// a currency such as NAD must keep its initial N.
			value = value[1:]
		}
		if field.tag == "90B" || field.tag == "90F" {
			// Price amount type precedes the currency, e.g. ACTU/USD12,50.
			_, amount, ok := strings.Cut(value, "/")
			if !ok {
				return fmt.Errorf("%s: missing price currency separator /", location)
			}
			return currencyAt(amount, 0)
		}
		err = currencyAt(value, 0)
		if field.tag == "92B" {
			_, second, ok := strings.Cut(value, "/")
			if !ok {
				return errors.Join(err, fmt.Errorf("%s: missing second exchange-rate currency", location))
			}
			err = errors.Join(err, currencyAt(second, 0))
		}
		return err
	case "50F", "59F":
		return v.validateMTPartyCountries(field, location)
	case "77B":
		var errs []error
		for _, line := range field.lines {
			for _, marker := range []string{"/ORDERRES/", "/BENEFRES/"} {
				if strings.HasPrefix(line, marker) {
					country, _, _ := strings.Cut(strings.TrimPrefix(line, marker), "/")
					errs = append(errs, v.checkCountry(country, location))
				}
			}
		}
		return errors.Join(errs...)
	case "94C":
		value, err := qualified()
		if err != nil {
			return err
		}
		return v.checkCountry(value, location)
	case "94F":
		value, err := qualified()
		if err != nil {
			return err
		}
		_, bic, ok := strings.Cut(value, "/")
		if !ok {
			return fmt.Errorf("%s: missing place identifier separator /", location)
		}
		return v.checkBIC(bic, location)
	}
	return nil
}

func (v *Validator) validateMTPartyCountries(field mtField, location string) error {
	var errs []error
	if field.tag == "50F" && !strings.HasPrefix(field.lines[0], "/") {
		// Party identifier: identification type / country / identifier.
		parts := strings.SplitN(field.lines[0], "/", 3)
		if len(parts) == 3 && len(parts[0]) == 4 {
			errs = append(errs, v.checkCountry(parts[1], location+" party identifier"))
		}
	}
	seenAddressCountry := false
	for index, line := range field.lines {
		if len(line) < 2 || line[1] != '/' {
			continue
		}
		countryLine := line[0] == '3'
		if field.tag == "50F" && strings.ContainsRune("567", rune(line[0])) {
			countryLine = true
		}
		if !countryLine {
			continue
		}
		// Only the first 3/ line starts with a country; later 3/ lines may
		// continue a long town/address and have no country subfield.
		if line[0] == '3' && seenAddressCountry {
			continue
		}
		if line[0] == '3' {
			seenAddressCountry = true
		}
		country, _, _ := strings.Cut(line[2:], "/")
		errs = append(errs, v.checkCountry(country, fmt.Sprintf("%s address line %d", location, index+1)))
	}
	return errors.Join(errs...)
}
