package messaging

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Pocketwind/SWIFT-Launcher/config"
	"github.com/Pocketwind/SWIFT-Launcher/logging"
	"github.com/Pocketwind/SWIFT-Launcher/validation"
	"github.com/antchfx/xmlquery"
)

const validValidationMT = "{1:F01BANKKRSEAXXX0000000000}{2:I103OTHRGB2LAXXXN}{4:\n:20:TESTREF\n:32A:261027USD1,\n:57A:BANKKRSEXXX\n-}"

const validValidationMX = `<DataPDU xmlns:h="urn:iso:std:iso:20022:tech:xsd:head.001.001.02" xmlns:m="urn:iso:std:iso:20022:tech:xsd:pacs.008.001.08">
<Header><MessageIdentifier>pacs.008.001.08</MessageIdentifier><Sender><DN>cn=xxx,o=bankkrse,o=swift</DN></Sender><Receiver><DN>cn=xxx,o=othrgb2l,o=swift</DN></Receiver><SenderReference>TESTREF</SenderReference><NetworkInfo><Service>swift.test</Service></NetworkInfo></Header>
<Body><h:AppHdr><h:BICFI>BANKKRSEXXX</h:BICFI></h:AppHdr><m:Document><m:Amt Ccy="USD">1</m:Amt><m:Ctry>KR</m:Ctry><m:BICFI>OTHRGB2L</m:BICFI></m:Document></Body></DataPDU>`

func setupValidationData(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	for _, subdir := range []string{"data/ref", "data/sr"} {
		if err := os.MkdirAll(filepath.Join(dir, subdir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"data/ref/bic.csv":            "BIC8,BIC11\nBANKKRSE,BANKKRSEXXX\nOTHRGB2L,OTHRGB2LXXX\n",
		"data/ref/currency.csv":       "Code,Name\nUSD,US Dollar\nKRW,Won\n",
		"data/ref/country.csv":        "Code,Name\nKR,Korea\nGB,United Kingdom\nUS,United States\n",
		"data/sr/head.001.001.02.xsd": `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:iso:std:iso:20022:tech:xsd:head.001.001.02" elementFormDefault="qualified"><xs:element name="AppHdr"><xs:complexType><xs:sequence><xs:element name="BICFI" type="xs:string"/></xs:sequence></xs:complexType></xs:element></xs:schema>`,
		"data/sr/pacs.008.001.08.xsd": `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:iso:std:iso:20022:tech:xsd:pacs.008.001.08" elementFormDefault="qualified"><xs:element name="Document"><xs:complexType><xs:sequence><xs:element name="Amt"><xs:complexType><xs:simpleContent><xs:extension base="xs:decimal"><xs:attribute name="Ccy" type="xs:string" use="required"/></xs:extension></xs:simpleContent></xs:complexType></xs:element><xs:element name="Ctry" type="xs:string"/><xs:element name="BICFI" type="xs:string"/></xs:sequence></xs:complexType></xs:element></xs:schema>`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(dir)
}

func TestMXDataMakerValidatesAndPreservesInheritedNamespaces(t *testing.T) {
	setupValidationData(t)
	path := filepath.Join(t.TempDir(), "message.xml")
	if err := os.WriteFile(path, []byte(validValidationMX), 0600); err != nil {
		t.Fatal(err)
	}
	data, err := MXDataMaker(path, make(chan logging.LogData, 8))
	if err != nil {
		t.Fatal(err)
	}
	v, err := validation.Default()
	if err != nil {
		t.Fatal(err)
	}
	if err := v.ValidateMX(data.Payload); err != nil {
		t.Fatalf("extracted payload lost its schema or namespace bindings: %v", err)
	}
	doc, err := xmlquery.Parse(strings.NewReader(data.Payload))
	if err != nil {
		t.Fatal(err)
	}
	if node := xmlquery.FindOne(doc, "//*[local-name()='Document']"); node == nil || node.NamespaceURI != "urn:iso:std:iso:20022:tech:xsd:pacs.008.001.08" {
		t.Fatalf("Document namespace changed: %+v", node)
	}
}

func TestInvalidInputsAreArchivedBeforeAnySubmission(t *testing.T) {
	setupValidationData(t)
	for _, tc := range []struct {
		name, kind, original, wantError string
	}{
		{"MT currency", "fin", strings.Replace(validValidationMT, "USD", "ZZZ", 1), "currency"},
		{"MT BIC", "fin", strings.Replace(validValidationMT, ":57A:BANKKRSEXXX", ":57A:NONEKRSEXXX", 1), "BIC"},
		{"MX currency", "interAct", strings.Replace(validValidationMX, `Ccy="USD"`, `Ccy="ZZZ"`, 1), "currency"},
		{"MX country", "interAct", strings.Replace(validValidationMX, ">KR<", ">ZZ<", 1), "country"},
		{"MX BIC", "interAct", strings.Replace(validValidationMX, ">OTHRGB2L<", ">BAD<", 1), "BIC"},
		{"MX XSD", "interAct", strings.ReplaceAll(validValidationMX, "m:Amt", "m:Unexpected"), "Document XSD"},
		{"MX missing schema", "interAct", strings.ReplaceAll(validValidationMX, "pacs.008.001.08", "pacs.999.001.01"), "XSD"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			partner := &config.Partner{Name: "unit", ProgressPath: dir, ErrorPath: filepath.Join(dir, "error")}
			path := filepath.Join(dir, "message.dat")
			if err := os.WriteFile(path, []byte(tc.original), 0600); err != nil {
				t.Fatal(err)
			}
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
			defer server.Close()
			settings := senderTestSettings(server)
			var err error
			if tc.kind == "fin" {
				err = processMTFile(settings, path, partner, nil, make(chan logging.LogData, 16), false)
			} else {
				err = processMXFile(settings, path, partner, nil, make(chan logging.LogData, 16), false)
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("expected %s validation error, got %v", tc.wantError, err)
			}
			wantLog := err.Error() + "\n"
			if calls.Load() != 0 {
				t.Fatalf("invalid message submitted %d times", calls.Load())
			}
			archives, err := filepath.Glob(filepath.Join(partner.ErrorPath, "failed-*", "message.dat"))
			if err != nil || len(archives) != 1 {
				t.Fatalf("validation failure was not archived: %v, %v", archives, err)
			}
			original, err := os.ReadFile(archives[0])
			if err != nil || string(original) != tc.original {
				t.Fatalf("invalid original was changed or lost: %v", err)
			}
			failureLog, err := os.ReadFile(archives[0] + ".log")
			if err != nil || string(failureLog) != wantLog {
				t.Fatalf("validation failure log does not match processing error: %q, %v", failureLog, err)
			}
		})
	}
}

func TestMissingReferenceDataBlocksInput(t *testing.T) {
	t.Chdir(t.TempDir())
	path := filepath.Join(t.TempDir(), "message.fin")
	if err := os.WriteFile(path, []byte(validValidationMT), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := MTDataMaker(path, make(chan logging.LogData, 4)); err == nil || !strings.Contains(err.Error(), "bic.csv") {
		t.Fatalf("missing reference data did not block input: %v", err)
	}
}

func TestMXDataMakerRejectsPayloadOutsideSelectedBody(t *testing.T) {
	setupValidationData(t)
	for _, message := range []string{
		strings.NewReplacer("<Body>", "<Body/><Other>", "</Body>", "</Other>").Replace(validValidationMX),
		strings.Replace(validValidationMX, "</h:AppHdr>", "</h:AppHdr></Body><Body>", 1),
	} {
		path := filepath.Join(t.TempDir(), "message.xml")
		if err := os.WriteFile(path, []byte(message), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := MXDataMaker(path, make(chan logging.LogData, 4)); err == nil || !strings.Contains(err.Error(), "Body") {
			t.Fatalf("accepted roots outside the selected Body: %v", err)
		}
	}
}

func TestMTDataMakerExtractsValidatedOutputHeaderAddresses(t *testing.T) {
	setupValidationData(t)
	message := "{1:F01OTHRGB2LAXXX0000000000}{2:O1031200261027BANKKRSEAXXX00000000002610271200N}{4:\n:20:TESTREF\n-}"
	path := filepath.Join(t.TempDir(), "message.fin")
	if err := os.WriteFile(path, []byte(message), 0600); err != nil {
		t.Fatal(err)
	}
	data, err := MTDataMaker(path, make(chan logging.LogData, 4))
	if err != nil {
		t.Fatal(err)
	}
	if data.Sender != "BANKKRSEAXXX" || data.Receiver != "OTHRGB2LAXXX" || data.MessageType != "fin.103" {
		t.Fatalf("parsed addresses differ from validated addresses: %+v", data)
	}
}

func TestDataMakersAcceptTestBICsWithoutChangingInput(t *testing.T) {
	setupValidationData(t)
	testBICs := strings.NewReplacer("BANKKRSE", "BANKKRS0", "OTHRGB2L", "OTHRGB20")

	t.Run("MT", func(t *testing.T) {
		original := testBICs.Replace(validValidationMT)
		path := filepath.Join(t.TempDir(), "message.fin")
		if err := os.WriteFile(path, []byte(original), 0600); err != nil {
			t.Fatal(err)
		}
		data, err := MTDataMaker(path, make(chan logging.LogData, 4))
		if err != nil {
			t.Fatalf("test BIC derived from registered live BIC rejected: %v", err)
		}
		if data.Sender != "BANKKRS0AXXX" || data.Receiver != "OTHRGB20AXXX" || !strings.Contains(data.Payload, ":57A:BANKKRS0XXX") {
			t.Fatalf("test BICs changed during parsing: %+v", data)
		}
		contents, err := os.ReadFile(path)
		if err != nil || string(contents) != original {
			t.Fatalf("input file changed: %v", err)
		}
	})

	t.Run("MX", func(t *testing.T) {
		original := testBICs.Replace(validValidationMX)
		path := filepath.Join(t.TempDir(), "message.xml")
		if err := os.WriteFile(path, []byte(original), 0600); err != nil {
			t.Fatal(err)
		}
		data, err := MXDataMaker(path, make(chan logging.LogData, 4))
		if err != nil {
			t.Fatalf("test BIC derived from registered live BIC rejected: %v", err)
		}
		if !strings.Contains(data.Payload, ">BANKKRS0XXX<") || !strings.Contains(data.Payload, ">OTHRGB20<") {
			t.Fatalf("test BICs changed in MX payload: %s", data.Payload)
		}
		contents, err := os.ReadFile(path)
		if err != nil || string(contents) != original {
			t.Fatalf("input file changed: %v", err)
		}
	})
}

func TestDataMakersRejectUnregisteredTestBICBranches(t *testing.T) {
	setupValidationData(t)
	for _, tc := range []struct{ name, original string }{
		{"MT", strings.Replace(validValidationMT, ":57A:BANKKRSEXXX", ":57A:BANKKRS0123", 1)},
		{"MX", strings.Replace(validValidationMX, ">BANKKRSEXXX<", ">BANKKRS0123<", 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "message.dat")
			if err := os.WriteFile(path, []byte(tc.original), 0600); err != nil {
				t.Fatal(err)
			}
			var err error
			if tc.name == "MT" {
				_, err = MTDataMaker(path, make(chan logging.LogData, 4))
			} else {
				_, err = MXDataMaker(path, make(chan logging.LogData, 4))
			}
			if err == nil || !strings.Contains(err.Error(), "BANKKRS0123") {
				t.Fatalf("unregistered test BIC branch accepted: %v", err)
			}
		})
	}
}
