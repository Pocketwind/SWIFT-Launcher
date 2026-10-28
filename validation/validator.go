// Package validation checks outgoing SWIFT messages against local reference data
// and ISO 20022 schemas.
package validation

import (
	"bufio"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/knroy/go-xml/xsd"
)

// Validator can be shared by collectors. Reference data is immutable after Load;
// schemas are compiled on first use and cached for the lifetime of the validator.
type Validator struct {
	bic8       map[string]struct{}
	bic11      map[string]struct{}
	currencies map[string]struct{}
	countries  map[string]struct{}
	schemaDir  string
	schemaMu   sync.Mutex
	schemas    map[string]*xsd.Schema
}

var defaults = struct {
	sync.Mutex
	byDirectory map[string]*Validator
}{byDirectory: make(map[string]*Validator)}

// Default uses data/ref and data/sr under the current working directory. Failed
// loads are retried, so correcting a missing reference file can unblock inputs.
func Default() (*Validator, error) {
	dataDir, err := filepath.Abs("data")
	if err != nil {
		return nil, fmt.Errorf("locating validation data: %w", err)
	}
	defaults.Lock()
	defer defaults.Unlock()
	if v := defaults.byDirectory[dataDir]; v != nil {
		return v, nil
	}
	v, err := Load(filepath.Join(dataDir, "ref"), filepath.Join(dataDir, "sr"))
	if err != nil {
		return nil, err
	}
	defaults.byDirectory[dataDir] = v
	return v, nil
}

// Load reads bic.csv (BIC8, BIC11), currency.csv (Code), and country.csv (Code).
// An unavailable schema is reported when an MX message needs that schema; MT
// inputs do not require XSD files.
func Load(refDir, schemaDir string) (*Validator, error) {
	bics, err := readColumns(filepath.Join(refDir, "bic.csv"), "BIC8", "BIC11")
	if err != nil {
		return nil, err
	}
	addTestBICVariants(bics[0], 8)
	addTestBICVariants(bics[1], 11)
	currencies, err := readColumns(filepath.Join(refDir, "currency.csv"), "Code")
	if err != nil {
		return nil, err
	}
	countries, err := readColumns(filepath.Join(refDir, "country.csv"), "Code")
	if err != nil {
		return nil, err
	}
	return &Validator{
		bic8: bics[0], bic11: bics[1], currencies: currencies[0], countries: countries[0],
		schemaDir: schemaDir, schemas: make(map[string]*xsd.Schema),
	}, nil
}

// Accept the zero in the eighth position used by test BICs, provided the
// remaining characters (including a BIC11 branch) match a CSV entry.
func addTestBICVariants(codes map[string]struct{}, length int) {
	for code := range codes {
		if len(code) == length && code[7] != '0' {
			codes[code[:7]+"0"+code[8:]] = struct{}{}
		}
	}
}

func readColumns(path string, names ...string) ([]map[string]struct{}, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("loading reference CSV %s: %w", path, err)
	}
	defer file.Close()
	input := bufio.NewReader(file)
	if prefix, _ := input.Peek(3); string(prefix) == "\uFEFF" {
		_, _ = input.Discard(3)
	}
	reader := csv.NewReader(input)
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("reading reference CSV header %s: %w", path, err)
	}
	indexes := make([]int, len(names))
	columns := make([]map[string]struct{}, len(names))
	for i, name := range names {
		indexes[i] = -1
		for j, label := range header {
			label = strings.TrimSpace(strings.TrimPrefix(label, "\uFEFF"))
			if label == name {
				if indexes[i] != -1 {
					return nil, fmt.Errorf("reference CSV %s has duplicate column %q", path, name)
				}
				indexes[i] = j
			}
		}
		if indexes[i] == -1 {
			return nil, fmt.Errorf("reference CSV %s is missing column %q", path, name)
		}
		columns[i] = make(map[string]struct{})
	}
	for rowNumber := 2; ; rowNumber++ {
		row, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading reference CSV %s row %d: %w", path, rowNumber, err)
		}
		for i, index := range indexes {
			value := strings.TrimSpace(row[index])
			if value != "" {
				columns[i][value] = struct{}{}
			}
		}
	}
	for i, column := range columns {
		if len(column) == 0 {
			return nil, fmt.Errorf("reference CSV %s column %q has no codes", path, names[i])
		}
	}
	return columns, nil
}

func (v *Validator) checkBIC(value, location string) error {
	value = strings.TrimSpace(value)
	var codes map[string]struct{}
	switch len(value) {
	case 8:
		codes = v.bic8
	case 11:
		codes = v.bic11
	default:
		return fmt.Errorf("%s: invalid BIC %q (requires 8 or 11 characters)", location, value)
	}
	var errs []error
	if _, ok := codes[value]; !ok {
		errs = append(errs, fmt.Errorf("%s: BIC %q is not in bic.csv", location, value))
	}
	errs = append(errs, v.checkCountry(value[4:6], location+" BIC country"))
	return errors.Join(errs...)
}

func (v *Validator) checkCurrency(value, location string) error {
	value = strings.TrimSpace(value)
	if _, ok := v.currencies[value]; !ok {
		return fmt.Errorf("%s: currency %q is not in currency.csv", location, value)
	}
	return nil
}

func (v *Validator) checkCountry(value, location string) error {
	value = strings.TrimSpace(value)
	if _, ok := v.countries[value]; !ok {
		return fmt.Errorf("%s: country %q is not in country.csv", location, value)
	}
	return nil
}
