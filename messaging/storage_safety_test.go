package messaging

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Pocketwind/SWIFT-Launcher/auth"
	"github.com/Pocketwind/SWIFT-Launcher/config"
	"github.com/Pocketwind/SWIFT-Launcher/logging"
)

func storageTestDB(t *testing.T) *sql.DB {
	t.Helper()
	t.Chdir(t.TempDir())
	DBOnce = sync.Once{}
	DB, DBErr = nil, nil
	db, err := getDB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		DBOnce = sync.Once{}
		DB, DBErr = nil, nil
	})
	return db
}

func storageTestToken() *auth.TokenData {
	return &auth.TokenData{AccessToken: "test-token", TokenType: "Bearer", ExpireTime: time.Now().Add(time.Hour).Unix()}
}

func storageTestMT(id int) MTDownload {
	return MTDownload{
		Distribution: Distribution{ID: id, Service: "fin", CloudReference: "test-reference"},
		Message: MTMessage{
			Payload:     base64.StdEncoding.EncodeToString([]byte(":20:TEST\n:32A:261007KRW1,")),
			MessageType: "fin.103", SenderReference: "TEST", Direction: "Incoming",
			Sender: "TESTKRSEAXXX", Receiver: "TESTKRSEBXXX", NetworkInfo: NetworkInfo{NetworkPriority: "Normal"},
		},
	}
}

func TestDownloadFINAcknowledgesFileDespiteDBFailure(t *testing.T) {
	db := storageTestDB(t)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]MTDownload{storageTestMT(1)})
	}))
	defer server.Close()
	output := t.TempDir()
	settings := &config.Settings{Messaging: config.Messaging{FinMessageUrl: server.URL, HttpClient: server.Client()}}
	logs := make(chan logging.LogData, 32)
	ids, err := downloadFINMessages(settings, storageTestToken(), []string{"1"}, []config.Partner{{Name: "test", OutputPath: output, Extension: ".fin"}}, logs)
	if err != nil || !reflect.DeepEqual(ids, []string{"1"}) {
		t.Fatalf("optional DB failure blocked delivery: ids=%v err=%v", ids, err)
	}
	if data, err := os.ReadFile(filepath.Join(output, "1.fin")); err != nil || len(data) == 0 {
		t.Fatalf("consumer file missing: %v", err)
	}
	close(logs)
	warned := false
	for log := range logs {
		if log.Type == "WARN" && strings.Contains(log.Text, "archive failed") {
			warned = true
		}
	}
	if !warned {
		t.Fatal("optional archive failure was not logged")
	}
}

func TestDownloadReportsAndMXIgnoreOptionalDBFailure(t *testing.T) {
	mt := storageTestMT(1)
	mx := MXDownload{
		Distribution: mt.Distribution,
		Message: MXMessage{
			Requestor:   "cn=xxx,ou=payments,o=testkrse,o=swift",
			Responder:   "cn=xxx,o=otherbic,o=swift",
			MessageType: "pacs.008.001.08", Format: "MX", Direction: "Incoming",
			Payload: base64.StdEncoding.EncodeToString([]byte("<Envelope><AppHdr/><Document/></Envelope>")),
		},
	}
	for _, tc := range []struct {
		name     string
		response any
		download func(*config.Settings, *auth.TokenData, []string, []config.Partner, chan<- logging.LogData) ([]string, error)
	}{
		{"MX message", []MXDownload{mx}, downloadInterActMessages},
		{"MX report", []MXReport{{Distribution: mx.Distribution, TransmissionReport: MXTransmissionReport{
			Message: mx.Message, ResponseDate: "2026-10-07T00:00:00Z", DeliveryStatus: "Acked",
			Report: base64.StdEncoding.EncodeToString([]byte("ack")),
		}}}, downloadInterActReports},
		{"FIN report", []MTReport{{Distribution: mt.Distribution, TransmissionReport: MTTransmissionReport{
			Message: mt.Message, ResponseDate: "2026-10-07T00:00:00Z", DeliveryStatus: "Acked",
		}}}, downloadFINReports},
	} {
		for _, fileFails := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/fileFails=%t", tc.name, fileFails), func(t *testing.T) {
				if err := storageTestDB(t).Close(); err != nil {
					t.Fatal(err)
				}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(tc.response) }))
				defer server.Close()
				output := t.TempDir()
				path := filepath.Join(output, "1.xml")
				if fileFails {
					if err := os.Mkdir(path, 0700); err != nil {
						t.Fatal(err)
					}
				}
				settings := &config.Settings{Messaging: config.Messaging{
					InterActMessageUrl: server.URL, InterActReportUrl: server.URL, FinReportUrl: server.URL, HttpClient: server.Client(),
				}}
				ids, err := tc.download(settings, storageTestToken(), []string{"1"}, []config.Partner{{Name: "test", OutputPath: output, AckPath: output, Extension: ".xml"}}, make(chan logging.LogData, 32))
				if fileFails {
					if err == nil || len(ids) != 0 {
						t.Fatalf("file failure acknowledged: ids=%v err=%v", ids, err)
					}
				} else {
					if err != nil || !reflect.DeepEqual(ids, []string{"1"}) {
						t.Fatalf("optional DB failure blocked delivery: ids=%v err=%v", ids, err)
					}
					if data, err := os.ReadFile(path); err != nil || len(data) == 0 {
						t.Fatalf("delivery file missing: %v", err)
					}
				}
			})
		}
	}
}

func TestDownloadFINAcknowledgesOnlyValidRequestedMessages(t *testing.T) {
	storageTestDB(t)
	valid, invalid, unexpected := storageTestMT(1), storageTestMT(2), storageTestMT(99)
	invalid.Message.Payload = base64.StdEncoding.EncodeToString([]byte("not a FIN field"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]MTDownload{valid, invalid, unexpected})
	}))
	defer server.Close()
	output := t.TempDir()
	settings := &config.Settings{Messaging: config.Messaging{FinMessageUrl: server.URL, HttpClient: server.Client()}}
	ids, err := downloadFINMessages(settings, storageTestToken(), []string{"1", "2"}, []config.Partner{{Name: "test", OutputPath: output, Extension: ".fin"}}, make(chan logging.LogData, 32))
	if err == nil || !reflect.DeepEqual(ids, []string{"1"}) {
		t.Fatalf("unexpected download result: ids=%v err=%v", ids, err)
	}
	if _, err := os.Stat(filepath.Join(output, "1.fin")); err != nil {
		t.Fatal(err)
	}
	files, err := os.ReadDir(output)
	if err != nil || len(files) != 1 {
		t.Fatalf("invalid message produced a file: files=%v err=%v", files, err)
	}
}

func TestDownloadFINRejectsHTTPErrorWithValidJSONBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode([]MTDownload{storageTestMT(1)})
	}))
	defer server.Close()
	settings := &config.Settings{Messaging: config.Messaging{FinMessageUrl: server.URL, HttpClient: server.Client()}}
	ids, err := downloadFINMessages(settings, storageTestToken(), []string{"1"}, nil, make(chan logging.LogData, 8))
	if err == nil || len(ids) != 0 || !strings.Contains(err.Error(), "503") {
		t.Fatalf("HTTP failure accepted: ids=%v err=%v", ids, err)
	}
}

func TestDownloadFINDoesNotAcknowledgeFilePublishFailure(t *testing.T) {
	storageTestDB(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]MTDownload{storageTestMT(1)})
	}))
	defer server.Close()
	output := t.TempDir()
	// A directory at the destination reliably rejects publishing the final file.
	if err := os.Mkdir(filepath.Join(output, "1.fin"), 0755); err != nil {
		t.Fatal(err)
	}
	settings := &config.Settings{Messaging: config.Messaging{FinMessageUrl: server.URL, HttpClient: server.Client()}}
	ids, err := downloadFINMessages(settings, storageTestToken(), []string{"1"}, []config.Partner{{Name: "test", OutputPath: output, Extension: ".fin"}}, make(chan logging.LogData, 32))
	if err == nil || len(ids) != 0 {
		t.Fatalf("file publish failure acknowledged: ids=%v err=%v", ids, err)
	}
}

func TestDownloadAcknowledgesSuccessBeforeLaterFileActFailure(t *testing.T) {
	var server *httptest.Server
	var acknowledged []int
	var ackMu sync.Mutex
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/initiate":
			if r.URL.Query().Get("distribution-id") == "2" {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			json.NewEncoder(w).Encode(Distribution{ID: 1, CompanionInfo: CompanionInfo{SenderReference: "received"}, FileTransferResponse: FileTransferResponse{SignedURLs: []SignedURL{{URL: server.URL + "/file"}}}})
		case "/file":
			io.WriteString(w, "complete file")
		case "/distributions":
			var items []struct {
				ID int `json:"id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&items); err != nil {
				t.Error(err)
			}
			ackMu.Lock()
			for _, item := range items {
				acknowledged = append(acknowledged, item.ID)
			}
			ackMu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	output := t.TempDir()
	settings := &config.Settings{Messaging: config.Messaging{FileActMessageUrl: server.URL + "/initiate", DistributionUrl: server.URL + "/distributions", HttpClient: server.Client()}}
	partners := []config.Partner{{Name: "test", Type: "fileAct", Direction: "out", Status: true, OutputPath: output, Extension: ".bin"}}
	err := Download(settings, storageTestToken(), []Distribution{{ID: 1, Service: "fileAct", Type: "message"}, {ID: 2, Service: "fileAct", Type: "message"}}, partners, make(chan logging.LogData, 32))
	ackMu.Lock()
	defer ackMu.Unlock()
	if err == nil || !reflect.DeepEqual(acknowledged, []int{1}) {
		t.Fatalf("successful download ACK lost after failure: acknowledged=%v err=%v", acknowledged, err)
	}
	data, err := os.ReadFile(filepath.Join(output, "received.bin"))
	if err != nil || string(data) != "complete file" {
		t.Fatalf("downloaded file: %q, err=%v", data, err)
	}
}

type storageRoundTripper func(*http.Request) (*http.Response, error)

func (f storageRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type storageBrokenReader struct{}

func (storageBrokenReader) Read(p []byte) (int, error) {
	return copy(p, "partial"), io.ErrUnexpectedEOF
}

func TestDownloadFileActInterruptedBodyKeepsExistingFile(t *testing.T) {
	output := t.TempDir()
	path := filepath.Join(output, "received.bin")
	if err := os.WriteFile(path, []byte("previous complete file"), 0644); err != nil {
		t.Fatal(err)
	}
	initiate, err := json.Marshal(Distribution{CompanionInfo: CompanionInfo{SenderReference: "received"}, FileTransferResponse: FileTransferResponse{SignedURLs: []SignedURL{{URL: "https://download.invalid/file"}}}})
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: storageRoundTripper(func(r *http.Request) (*http.Response, error) {
		body := io.NopCloser(strings.NewReader(string(initiate)))
		if r.Method == http.MethodGet {
			body = io.NopCloser(storageBrokenReader{})
		}
		return &http.Response{StatusCode: http.StatusOK, Body: body, Header: make(http.Header)}, nil
	})}
	settings := &config.Settings{Messaging: config.Messaging{FileActMessageUrl: "https://initiate.invalid", HttpClient: client}}
	ids, err := downloadFileActMessages(settings, storageTestToken(), []string{"1"}, []config.Partner{{OutputPath: output, Extension: ".bin"}}, make(chan logging.LogData, 32))
	if err == nil || len(ids) != 0 {
		t.Fatalf("interrupted download acknowledged: ids=%v err=%v", ids, err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "previous complete file" {
		t.Fatalf("interrupted download replaced complete file: %q err=%v", data, err)
	}
}

func TestDownloadFileActRejectsMultipartBeforePublishingOrACK(t *testing.T) {
	requests := 0
	client := &http.Client{Transport: storageRoundTripper(func(r *http.Request) (*http.Response, error) {
		requests++
		body := `{"file_transfer_response":{"signed_urls":[{"part":1,"url":"https://download.invalid/one"},{"part":2,"url":"https://download.invalid/two"}]}}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	settings := &config.Settings{Messaging: config.Messaging{FileActMessageUrl: "https://initiate.invalid", HttpClient: client}}
	ids, err := downloadFileActMessages(settings, storageTestToken(), []string{"1"}, nil, make(chan logging.LogData, 32))
	if err == nil || len(ids) != 0 || requests != 1 {
		t.Fatalf("multipart transfer accepted: ids=%v err=%v requests=%d", ids, err, requests)
	}
}

func TestDistributionOutputPathRejectsEscapes(t *testing.T) {
	base := t.TempDir()
	for _, pair := range [][2]string{{"../outside", "1.fin"}, {`..\outside`, "1.fin"}, {"/outside", "1.fin"}, {"", "../1.fin"}, {"", `C:\1.fin`}, {"", ""}} {
		if path, err := distributionOutputPath(base, pair[0], pair[1]); err == nil {
			t.Errorf("unsafe path accepted: tag=%q name=%q path=%q", pair[0], pair[1], path)
		}
	}
	path, err := distributionOutputPath(base, "valid/tag", "1.fin")
	if err != nil || path != filepath.Join(base, "valid", "tag", "1.fin") {
		t.Fatalf("valid nested tag rejected: %q %v", path, err)
	}
}

func TestMTDatabaseFailureRollsBackMessageAndFields(t *testing.T) {
	for _, report := range []bool{false, true} {
		name := "message"
		if report {
			name = "report"
		}
		t.Run(name, func(t *testing.T) {
			db := storageTestDB(t)
			message := storageTestMT(1)
			message.Message.Payload = "original"
			message.Message.MT = MT{Line: []Field{{Field: "20", Data: "original"}}}
			write := func(message MTDownload) error {
				if report {
					return WriteMTReportToSQL(MTReport{Distribution: message.Distribution, TransmissionReport: MTTransmissionReport{Message: message.Message}}, "test")
				}
				return WriteMTMessageToSQL(message, "test")
			}
			if err := write(message); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`CREATE TRIGGER reject_field BEFORE INSERT ON mt_data WHEN NEW.field_tag = 'FAIL' BEGIN SELECT RAISE(ABORT, 'test failure'); END`); err != nil {
				t.Fatal(err)
			}
			message.Message.Payload = "changed"
			message.Message.MT = MT{Line: []Field{{Field: "20", Data: "changed"}, {Field: "FAIL", Data: "failure"}}}
			if err := write(message); err == nil {
				t.Fatal("expected field insert failure")
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			var rawText, field string
			if err := db.QueryRowContext(ctx, `SELECT raw_text, field_value FROM messages JOIN mt_data ON messages.id = mt_data.message_id WHERE distribution_id = 1`).Scan(&rawText, &field); err != nil {
				t.Fatalf("failed transaction did not release connection: %v", err)
			}
			if rawText != "original" || field != "original" {
				t.Fatalf("failed write changed existing data: raw=%q field=%q", rawText, field)
			}
		})
	}
}

func TestMXDatabaseFailureRollsBackMessageAndDocument(t *testing.T) {
	for _, report := range []bool{false, true} {
		name := "message"
		if report {
			name = "report"
		}
		t.Run(name, func(t *testing.T) {
			db := storageTestDB(t)
			message := MXDownload{Distribution: Distribution{ID: 1, CloudReference: "test-reference"}, Message: MXMessage{Payload: "original", MX: MX{AppHeader: "header", Document: "original"}}}
			write := func(message MXDownload) error {
				if report {
					return WriteMXReportToSQL(MXReport{Distribution: message.Distribution, TransmissionReport: MXTransmissionReport{Message: message.Message}}, "test")
				}
				return WriteMXMessageToSQL(message, "test")
			}
			if err := write(message); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`CREATE TRIGGER reject_document BEFORE INSERT ON mx_data WHEN NEW.document = 'FAIL' BEGIN SELECT RAISE(ABORT, 'test failure'); END`); err != nil {
				t.Fatal(err)
			}
			message.Message.Payload = "changed"
			message.Message.MX.Document = "FAIL"
			if err := write(message); err == nil {
				t.Fatal("expected document insert failure")
			}
			var rawText, document string
			if err := db.QueryRow(`SELECT raw_text, document FROM messages JOIN mx_data ON messages.id = mx_data.message_id WHERE distribution_id = 1`).Scan(&rawText, &document); err != nil {
				t.Fatal(err)
			}
			if rawText != "original" || document != "original" {
				t.Fatalf("failed write changed existing data: raw=%q document=%q", rawText, document)
			}
		})
	}
}
