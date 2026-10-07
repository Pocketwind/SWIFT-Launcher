package messaging

import (
	"bytes"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Pocketwind/SWIFT-Launcher/auth"
	"github.com/Pocketwind/SWIFT-Launcher/config"
	"github.com/Pocketwind/SWIFT-Launcher/fsutil"
	"github.com/Pocketwind/SWIFT-Launcher/logging"
	"github.com/golang-jwt/jwt/v5"
)

var testKeys struct {
	sync.Once
	privateKey  string
	certificate string
	err         error
}

func senderTestToken(t *testing.T) *auth.TokenData {
	t.Helper()
	testKeys.Do(func() {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			testKeys.err = err
			return
		}
		template := &x509.Certificate{
			SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "unit-test"},
			NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		}
		cert, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
		if err != nil {
			testKeys.err = err
			return
		}
		testKeys.privateKey = string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
		testKeys.certificate = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert}))
	})
	if testKeys.err != nil {
		t.Fatal(testKeys.err)
	}
	return &auth.TokenData{
		AccessToken: "test-access-token", TokenType: "Bearer", ExpireTime: time.Now().Add(time.Hour).Unix(),
		TokenPurpose: "messaging", PrivateKey: testKeys.privateKey, PublicKey: testKeys.certificate,
	}
}

func senderTestSettings(server *httptest.Server) *config.Settings {
	return &config.Settings{Messaging: config.Messaging{
		FinMessageUrl: server.URL, InterActMessageUrl: server.URL, FileActUrl: server.URL,
		FileActAckUrl: server.URL, Subject: "unit-test", HttpClient: server.Client(),
	}}
}

func TestSenderChecksStatusAndHoldsAmbiguousResponses(t *testing.T) {
	for _, tc := range []struct {
		name          string
		status        int
		body          string
		wantUncertain bool
		wantReference string
	}{
		{"accepted", 201, `{"message_cloud_reference":"ref-123"}`, false, "ref-123"},
		{"rejected despite reference", 400, `{"message_cloud_reference":"fake"}`, false, ""},
		{"server failure", 503, `{}`, true, ""},
		{"conflict", 409, `{}`, true, ""},
		{"invalid accepted response", 201, `{`, true, ""},
		{"missing accepted reference", 201, `{}`, true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			}))
			defer server.Close()
			ref, err := MTSender(MTData{Payload: "unit-test"}, senderTestToken(t), senderTestSettings(server), make(chan logging.LogData, 32), false)
			if ref != tc.wantReference {
				t.Fatalf("reference = %q, want %q", ref, tc.wantReference)
			}
			if tc.wantReference != "" && err != nil || tc.wantReference == "" && err == nil {
				t.Fatalf("unexpected error: %v", err)
			}
			var uncertain *UncertainSendError
			if errors.As(err, &uncertain) != tc.wantUncertain {
				t.Fatalf("uncertain classification = %v, want %v: %v", uncertain != nil, tc.wantUncertain, err)
			}
			if calls.Load() != 1 {
				t.Fatalf("submission count = %d, want 1", calls.Load())
			}
		})
	}
}

func TestSenderHoldsResponseLossAfterServerAcceptance(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			conn.Close()
		}
	}))
	defer server.Close()
	_, err := MTSender(MTData{Payload: "unit-test"}, senderTestToken(t), senderTestSettings(server), make(chan logging.LogData, 32), false)
	var uncertain *UncertainSendError
	if !errors.As(err, &uncertain) || calls.Load() != 1 {
		t.Fatalf("response loss must be held without retry; calls=%d, err=%v", calls.Load(), err)
	}
}

func TestSendHTTPDoesNotFollowRedirect(t *testing.T) {
	var redirected atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected.Add(1) }))
	defer destination.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", destination.URL)
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	req, err := http.NewRequest(http.MethodPost, origin.URL, strings.NewReader("sensitive payload"))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := sendHTTP(origin.Client(), req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 307 || redirected.Load() != 0 {
		t.Fatalf("signed POST followed redirect: status=%d, redirected=%d", resp.StatusCode, redirected.Load())
	}
}

func TestLoadFileActPayloadUsesBodyAndValidatesDigest(t *testing.T) {
	dir := t.TempDir()
	payload := []byte("actual-body")
	if err := os.WriteFile(filepath.Join(dir, "body.dat"), payload, 0600); err != nil {
		t.Fatal(err)
	}
	companion := filepath.Join(dir, "companion.xml")
	if err := os.WriteFile(companion, []byte("<Document><Body>body.dat</Body><FileLogicalName>remote.dat</FileLogicalName></Document>"), 0600); err != nil {
		t.Fatal(err)
	}
	digest := md5.Sum(payload)
	data := FAData{FileTransferRequest: FileTransferRequest{FileAttributes: FileAttributes{
		FileName: "remote.dat", FileSize: len(payload), FileDigest: base64.StdEncoding.EncodeToString(digest[:]),
	}}}
	got, err := loadFileActPayload(data, companion, &config.Partner{InputPath: dir})
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("wrong upload file: got=%q, err=%v", got, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "body.dat"), []byte("mutatedbody"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadFileActPayload(data, companion, &config.Partner{InputPath: dir}); err == nil {
		t.Fatal("changed upload bytes were accepted")
	}
}

func TestFileActUploadRejectsEmptyAndMultipartURLs(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer server.Close()
	for _, urls := range [][]SignedURL{nil, {{URL: ""}}, {{URL: server.URL, Part: 1}, {URL: server.URL, Part: 2}}} {
		initiate := FileActInitiateResponse{FileTransferResponse: FileTransferResponse{SignedURLs: urls}}
		if err := fileActUploadPayload(FAData{}, initiate, []byte("body"), senderTestSettings(server)); err == nil {
			t.Fatalf("invalid upload URLs accepted: %+v", urls)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("invalid upload request reached server %d times", calls.Load())
	}
}

func TestFileActUploadUsesSnapshotContentLength(t *testing.T) {
	payload := []byte("immutable snapshot")
	var received []byte
	var receivedLength int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedLength = r.ContentLength
		received, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	initiate := FileActInitiateResponse{FileTransferResponse: FileTransferResponse{SignedURLs: []SignedURL{{URL: server.URL}}}}
	if err := fileActUploadPayload(FAData{}, initiate, payload, senderTestSettings(server)); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(received, payload) || receivedLength != int64(len(payload)) {
		t.Fatalf("upload bytes/length differ: body=%q, length=%d", received, receivedLength)
	}
}

func TestFileActCompleteSignsActualURLAndEmptyBody(t *testing.T) {
	token := senderTestToken(t)
	token.Subject = "certificate-subject"
	observed := make(chan *http.Request, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(body))
		observed <- r
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()
	settings := senderTestSettings(server)
	settings.Messaging.FileActAckUrl = server.URL + "/transfers/{transfer-id}?existing=value"
	if err := fileActComplete("transfer-123", token, settings, make(chan logging.LogData, 32)); err != nil {
		t.Fatal(err)
	}
	req := <-observed
	body, _ := io.ReadAll(req.Body)
	if len(body) != 0 || req.URL.Query().Get("existing") != "value" || req.URL.Query().Get("transfer-id") != "transfer-123" {
		t.Fatalf("completion request changed endpoint query/body: %s, %q", req.URL, body)
	}
	block, _ := pem.Decode([]byte(token.PublicKey))
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := jwt.Parse(req.Header.Get("X-SWIFT-Signature"), func(*jwt.Token) (any, error) { return cert.PublicKey, nil })
	if err != nil {
		t.Fatal(err)
	}
	claims := signature.Claims.(jwt.MapClaims)
	digest := sha256.Sum256(nil)
	serverURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	wantAudience := serverURL.Hostname() + req.URL.RequestURI()
	if claims["aud"] != wantAudience || claims["digest"] != base64.StdEncoding.EncodeToString(digest[:]) || claims["sub"] != token.Subject {
		t.Fatalf("signature does not describe transmitted URL/body and certificate subject: %+v", claims)
	}
}

func TestSenderStopsOnAuthenticationFailure(t *testing.T) {
	var submissions atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/token" {
			submissions.Add(1)
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	settings := senderTestSettings(server)
	settings.Messaging.TokenUrl = server.URL + "/token"
	token := senderTestToken(t)
	token.ExpireTime = 0
	token.RefreshToken = "expired-refresh-token"
	_, err := MTSender(MTData{Payload: "must not send"}, token, settings, make(chan logging.LogData, 32), false)
	if err == nil || submissions.Load() != 0 {
		t.Fatalf("authentication failure did not stop submission: calls=%d, err=%v", submissions.Load(), err)
	}
}

func TestErrorRouterPreservesSameNamedFailures(t *testing.T) {
	dir := t.TempDir()
	partner := &config.Partner{ErrorPath: filepath.Join(dir, "error")}
	source := filepath.Join(dir, "message.fin")
	for _, body := range []string{"first failed original", "second failed original"} {
		if err := os.WriteFile(source, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if err := ErrorMessageRouter(source, partner); err != nil {
			t.Fatal(err)
		}
	}
	paths, err := filepath.Glob(filepath.Join(partner.ErrorPath, "failed-*", "message.fin"))
	if err != nil || len(paths) != 2 {
		t.Fatalf("same-named failure was overwritten: %v, %v", paths, err)
	}
	bodies := make(map[string]bool)
	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		bodies[string(body)] = true
	}
	if !bodies["first failed original"] || !bodies["second failed original"] {
		t.Fatalf("failed originals were not preserved: %v", bodies)
	}
}

func TestCollectorHoldsUncertainSourceAndArchivesAcceptedSource(t *testing.T) {
	const original = "{1:F01BANKBICXXXXX0000000000}{2:I103BANKBICXXXXXN}{4:\n:20:TESTREF\n:32A:TEST\n-}"
	for _, accepted := range []bool{false, true} {
		t.Run(map[bool]string{false: "uncertain", true: "accepted"}[accepted], func(t *testing.T) {
			dir := t.TempDir()
			partner := &config.Partner{Name: "unit", ProgressPath: filepath.Join(dir, "progress"), ErrorPath: filepath.Join(dir, "error")}
			if err := os.Mkdir(partner.ProgressPath, 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(partner.ProgressPath, "message.fin")
			if err := os.WriteFile(path, []byte(original), 0600); err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if accepted {
					w.WriteHeader(201)
					io.WriteString(w, `{"message_cloud_reference":"ref-123"}`)
				} else {
					w.WriteHeader(503)
				}
			}))
			defer server.Close()
			err := processMTFile(senderTestSettings(server), path, partner, senderTestToken(t), make(chan logging.LogData, 32), false)
			if !accepted {
				var uncertain *UncertainSendError
				if !errors.As(err, &uncertain) {
					t.Fatalf("expected uncertain error: %v", err)
				}
				got, readErr := os.ReadFile(path)
				if readErr != nil || string(got) != original {
					t.Fatalf("uncertain source lost: %v", readErr)
				}
				if _, err := os.Stat(path + ".hold.json"); err != nil {
					t.Fatalf("held state missing: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			matches, err := filepath.Glob(filepath.Join(partner.ProgressPath, "sent", "*", "original", "message.fin"))
			if err != nil || len(matches) != 1 {
				t.Fatalf("accepted original archive missing: %v", err)
			}
			got, err := os.ReadFile(matches[0])
			if err != nil || string(got) != original {
				t.Fatalf("archived original differs: %v", err)
			}
			receiptPath := filepath.Join(filepath.Dir(filepath.Dir(matches[0])), "receipt.json")
			receipt, err := os.ReadFile(receiptPath)
			var metadata map[string]any
			if err != nil || json.Unmarshal(receipt, &metadata) != nil || metadata["reference"] != "ref-123" {
				t.Fatalf("accepted reference missing from receipt: %s, %v", receipt, err)
			}
		})
	}
}

func TestCollectorStopsWithClosedInputOrPendingShutdown(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		partner := &config.Partner{Status: true, InputChannel: make(chan string, 1), Extension: ".fin"}
		exit := make(chan bool)
		if shutdown {
			partner.InputChannel <- "unused.fin"
			close(exit)
		} else {
			close(partner.InputChannel)
		}
		done := make(chan struct{})
		go func() {
			CollectorService(nil, partner, nil, make(chan logging.LogData, 32), exit)
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("collector failed to stop")
		}
	}
}

func TestWatcherStartupDoesNotEnqueueProgressOrTemporaryFiles(t *testing.T) {
	dir := t.TempDir()
	partner := &config.Partner{Status: true, Direction: "in", Extension: ".tmp", InputPath: filepath.Join(dir, "input"),
		ProgressPath: filepath.Join(dir, "progress"), InputChannel: make(chan string, 8)}
	if err := os.Mkdir(partner.InputPath, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(partner.ProgressPath, 0700); err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(partner.InputPath, "ready.tmp")
	for _, path := range []string{input, filepath.Join(partner.ProgressPath, "uncertain.tmp"), filepath.Join(partner.InputPath, ".swift-staged.tmp")} {
		if err := os.WriteFile(path, []byte("unit-test"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	exit := make(chan bool)
	done := make(chan struct{})
	go func() {
		fsutil.WatchFileService(partner, exit, make(chan logging.LogData, 32))
		close(done)
	}()
	select {
	case got := <-partner.InputChannel:
		if got != input {
			t.Errorf("unexpected startup input %q", got)
		}
	case <-time.After(4 * time.Second):
		t.Error("startup input was not queued")
	}
	close(exit)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watcher failed to stop")
	}
	select {
	case got := <-partner.InputChannel:
		t.Fatalf("held or temporary file was queued: %s", got)
	default:
	}
}
