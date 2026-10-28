package validation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeReferenceFiles(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{
		"bic.csv":      "\uFEFF BIC8 , BIC11 ,Branch\n BANKKRSE , BANKKRSEXXX ,XXX\nOTHRGB2L,OTHRGB2LXXX,XXX\n",
		"currency.csv": "\uFEFF\"Code\",\"Name\"\n USD ,Dollar\nKRW,Won\n",
		"country.csv":  "Code,Name\nKR,Korea\nGB,United Kingdom\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLoadReferenceCSVsAndStrictMembership(t *testing.T) {
	dir := t.TempDir()
	writeReferenceFiles(t, dir)
	v, err := Load(dir, filepath.Join(dir, "no-schemas-needed-for-mt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, bic := range []string{"BANKKRSE", "BANKKRSEXXX", "OTHRGB2L"} {
		if err := v.checkBIC(bic, "test"); err != nil {
			t.Fatal(err)
		}
	}
	for _, bic := range []string{"BAD", "BANKKRSE123", "NONEKRSE", "bankkrse"} {
		if err := v.checkBIC(bic, "test"); err == nil {
			t.Errorf("invalid BIC %q accepted", bic)
		}
	}
	if err := v.checkCurrency("USD", "test"); err != nil {
		t.Fatal(err)
	}
	if err := v.checkCurrency("ZZZ", "test"); err == nil {
		t.Fatal("unknown currency accepted")
	}
	if err := v.checkCountry("ZZ", "test"); err == nil {
		t.Fatal("unknown country accepted")
	}
	delete(v.countries, "KR")
	if err := v.checkBIC("BANKKRSE", "test"); err == nil || !strings.Contains(err.Error(), "country") {
		t.Fatalf("BIC country not checked against country.csv: %v", err)
	}
}

func TestMalformedReferenceCSVsAreRejected(t *testing.T) {
	for _, tc := range []struct{ name, csv, want string }{
		{"missing column", "Other\nUSD\n", "missing column"},
		{"duplicate column", "Code,Code\nUSD,USD\n", "duplicate column"},
		{"empty codes", "Code,Name\n,Dollar\n", "no codes"},
		{"short row", "Code,Name\nUSD\n", "row 2"},
		{"malformed quote", "Code,Name\nUSD,\"Dollar\n", "row 2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "currency.csv")
			if err := os.WriteFile(path, []byte(tc.csv), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := readColumns(path, "Code"); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q, got %v", tc.want, err)
			}
		})
	}
}

func TestBICAllowsZeroInEighthPositionForRegisteredCodes(t *testing.T) {
	dir := t.TempDir()
	writeReferenceFiles(t, dir)
	v, err := Load(dir, filepath.Join(dir, "schemas"))
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"BANKKRS0", "BANKKRS0XXX", "OTHRGB20", "OTHRGB20XXX"} {
		if err := v.checkBIC(code, "test"); err != nil {
			t.Errorf("registered BIC with eighth-position zero %q rejected: %v", code, err)
		}
	}
	for _, code := range []string{"NONEKRS0", "NONEKRS0XXX", "BANKKRS0123", "BANKKRS9", "BANKKRS9XXX", "BAD0", "BANKKRS0XXXX"} {
		if err := v.checkBIC(code, "test"); err == nil {
			t.Errorf("unregistered or malformed BIC %q accepted", code)
		}
	}
	delete(v.countries, "KR")
	if err := v.checkBIC("BANKKRS0XXX", "test"); err == nil || !strings.Contains(err.Error(), "country") {
		t.Fatalf("zero in eighth position bypassed country validation: %v", err)
	}
}

func TestDefaultRetriesMissingDataAndSharesSuccessfulLoad(t *testing.T) {
	t.Chdir(t.TempDir())
	if _, err := Default(); err == nil {
		t.Fatal("missing reference files accepted")
	}
	writeReferenceFiles(t, filepath.Join("data", "ref"))
	v, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	again, err := Default()
	if err != nil || v != again {
		t.Fatalf("successful reference load not shared: %v", err)
	}
}
