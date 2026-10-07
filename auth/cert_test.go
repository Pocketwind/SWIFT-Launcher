package auth

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Pocketwind/SWIFT-Launcher/config"
	"github.com/Pocketwind/SWIFT-Launcher/logging"
)

func writeExistingTestPair(t *testing.T, directory string) {
	t.Helper()
	for name, value := range map[string]string{
		"channel.key": "existing-private-key",
		"channel.cer": "existing-certificate",
		"channel.csr": "existing-csr",
	} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func assertExistingTestPair(t *testing.T, directory string) {
	t.Helper()
	for name, expected := range map[string]string{
		"channel.key": "existing-private-key",
		"channel.cer": "existing-certificate",
		"channel.csr": "existing-csr",
	} {
		actual, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil || string(actual) != expected {
			t.Errorf("previous %s was modified or lost (read error: %v)", name, err)
		}
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Errorf("failed issuance or rollback left %d files; want 3", len(entries))
	}
}

func issuedCertificateHTML(t *testing.T, request *http.Request, signer *rsa.PrivateKey, matchCSR bool) string {
	t.Helper()
	if err := request.ParseForm(); err != nil {
		t.Fatal(err)
	}
	if request.Form.Get("action") != "getServerCert" || request.Form.Get("reference_number") != "reference" || request.Form.Get("authcode") != "local-authcode" {
		t.Fatal("certificate request omitted required form fields")
	}
	block, _ := pem.Decode([]byte(request.Form.Get("pkcs10Request")))
	if block == nil || block.Type != "CERTIFICATE REQUEST" {
		t.Fatal("certificate request does not contain an in-memory CSR")
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if err := csr.CheckSignature(); err != nil {
		t.Fatal(err)
	}
	publicKey := csr.PublicKey
	if !matchCSR {
		publicKey = &signer.PublicKey
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: csr.Subject,
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, publicKey, signer)
	if err != nil {
		t.Fatal(err)
	}
	return "<html><font>" + string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})) + "</font></html>"
}

func certificateTestSettings(respond func(*http.Request) (int, string)) *config.Settings {
	return &config.Settings{Messaging: config.Messaging{
		Subject: "configured-subject",
		HttpClient: &http.Client{Transport: tokenTestRoundTripper(func(request *http.Request) (*http.Response, error) {
			status, body := respond(request)
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		})},
	}}
}

func TestGetCertFailurePreservesExistingPair(t *testing.T) {
	signer, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		respond func(*http.Request) (int, string)
	}{
		{name: "HTTP failure", respond: func(*http.Request) (int, string) { return http.StatusUnauthorized, "rejected" }},
		{name: "missing certificate", respond: func(*http.Request) (int, string) { return http.StatusOK, "<html>rejected</html>" }},
		{name: "invalid PEM", respond: func(*http.Request) (int, string) { return http.StatusOK, "<font>invalid certificate</font>" }},
		{name: "mismatched certificate", respond: func(request *http.Request) (int, string) {
			return http.StatusOK, issuedCertificateHTML(t, request, signer, false)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			directory := t.TempDir()
			writeExistingTestPair(t, directory)
			settings := certificateTestSettings(tc.respond)
			token := &TokenData{PrivateKey: "old-memory-key", PublicKey: "old-memory-certificate", Subject: "old-subject"}
			err := getCert(settings, token, make(chan logging.LogData, 32), strings.NewReader("reference\nlocal-authcode\n"), directory)
			if err == nil {
				t.Fatal("failed or invalid certificate issuance was accepted")
			}
			assertExistingTestPair(t, directory)
			if token.PrivateKey != "old-memory-key" || token.PublicKey != "old-memory-certificate" || token.Subject != "old-subject" || settings.Messaging.Subject != "configured-subject" {
				t.Error("failed certificate issuance changed the active credentials")
			}
		})
	}
}

func TestGetCertInstallsMatchingPairAfterValidation(t *testing.T) {
	signer, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	for _, updateExistingToken := range []bool{true, false} {
		name := "initial credentials"
		if updateExistingToken {
			name = "renewed credentials"
		}
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			writeExistingTestPair(t, directory)
			settings := certificateTestSettings(func(request *http.Request) (int, string) {
				return http.StatusOK, issuedCertificateHTML(t, request, signer, true)
			})
			var token *TokenData
			if updateExistingToken {
				token = &TokenData{PrivateKey: "old-memory-key", PublicKey: "old-memory-certificate", Subject: "old-subject"}
			}
			if err := getCert(settings, token, make(chan logging.LogData, 32), strings.NewReader("reference\nlocal-authcode\n"), directory); err != nil {
				t.Fatal(err)
			}
			keyPEM, err := os.ReadFile(filepath.Join(directory, "channel.key"))
			if err != nil {
				t.Fatal(err)
			}
			certPEM, err := os.ReadFile(filepath.Join(directory, "channel.cer"))
			if err != nil {
				t.Fatal(err)
			}
			keyBlock, _ := pem.Decode(keyPEM)
			certBlock, _ := pem.Decode(certPEM)
			if keyBlock == nil || certBlock == nil {
				t.Fatal("installed credentials are not PEM encoded")
			}
			key, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
			if err != nil {
				t.Fatal(err)
			}
			certificate, err := x509.ParseCertificate(certBlock.Bytes)
			if err != nil {
				t.Fatal(err)
			}
			publicKey, ok := certificate.PublicKey.(*rsa.PublicKey)
			if !ok || publicKey.N.Cmp(key.PublicKey.N) != 0 || publicKey.E != key.PublicKey.E {
				t.Fatal("installed certificate and key do not match")
			}
			subject, err := GetCertDN(string(certPEM))
			if err != nil {
				t.Fatal(err)
			}
			if token != nil {
				if token.PrivateKey != string(keyPEM) || token.PublicKey != string(certPEM) || token.Subject != subject {
					t.Error("active credentials differ from the installed pair")
				}
				if settings.Messaging.Subject != "configured-subject" {
					t.Error("renewal mutated shared settings instead of token data")
				}
			} else if settings.Messaging.Subject != subject {
				t.Error("initial issuance did not configure the certificate subject")
			}
			csr, err := os.ReadFile(filepath.Join(directory, "channel.csr"))
			if err != nil || !bytes.Contains(csr, []byte("BEGIN CERTIFICATE REQUEST")) {
				t.Error("successful issuance did not retain its CSR")
			}
			entries, err := os.ReadDir(directory)
			if err != nil || len(entries) != 3 {
				t.Error("successful installation left temporary or backup files")
			}
		})
	}
}

func TestCertificateInstallationRollsBackFileFailures(t *testing.T) {
	for _, phase := range []string{"certificate backup", "key installation", "certificate installation"} {
		t.Run(phase, func(t *testing.T) {
			directory := t.TempDir()
			writeExistingTestPair(t, directory)
			injectedFailure := errors.New("simulated rename failure")
			rename := func(oldPath, newPath string) error {
				oldName, newName := filepath.Base(oldPath), filepath.Base(newPath)
				fail := phase == "certificate backup" && oldName == "channel.cer" && strings.HasPrefix(newName, "channel.cer.backup-")
				fail = fail || phase == "key installation" && strings.HasPrefix(oldName, "channel.key.new-") && newName == "channel.key"
				fail = fail || phase == "certificate installation" && strings.HasPrefix(oldName, "channel.cer.new-") && newName == "channel.cer"
				if fail {
					return injectedFailure
				}
				return os.Rename(oldPath, newPath)
			}
			err := installCertificatePair(directory, []byte("new-key"), []byte("new-certificate"), rename)
			if !errors.Is(err, injectedFailure) {
				t.Errorf("installation did not report its rename failure: %v", err)
			}
			assertExistingTestPair(t, directory)
		})
	}
}

func TestInitialCertificateInstallationRemovesPartialPair(t *testing.T) {
	directory := t.TempDir()
	rename := func(oldPath, newPath string) error {
		if strings.HasPrefix(filepath.Base(oldPath), "channel.cer.new-") {
			return errors.New("simulated certificate installation failure")
		}
		return os.Rename(oldPath, newPath)
	}
	if err := installCertificatePair(directory, []byte("new-key"), []byte("new-certificate"), rename); err == nil {
		t.Fatal("installation failure was accepted")
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 0 {
		t.Error("failed initial installation left a partial key/certificate pair")
	}
}

func TestGetCertPreservesBufferedConsoleCommands(t *testing.T) {
	settings := certificateTestSettings(func(*http.Request) (int, string) {
		return http.StatusUnauthorized, "rejected"
	})
	// A small reader must be reused, so its buffered commands remain available.
	reader := bufio.NewReaderSize(strings.NewReader("reference\nlocal-authcode\nexit\n"), 16)
	if err := getCert(settings, nil, make(chan logging.LogData, 32), reader, t.TempDir()); err == nil {
		t.Fatal("rejected certificate issuance was accepted")
	}
	command, err := reader.ReadString('\n')
	if err != nil || command != "exit\n" {
		t.Errorf("certificate prompts consumed the next command: %q, error %v", command, err)
	}
}

func TestGetCertPromptCancellationPreservesExistingPair(t *testing.T) {
	for _, prompt := range []string{"reference number", "authcode"} {
		t.Run(prompt, func(t *testing.T) {
			directory := t.TempDir()
			writeExistingTestPair(t, directory)
			settings := certificateTestSettings(func(*http.Request) (int, string) {
				t.Error("cancelled prompt started a certificate issuance request")
				return http.StatusInternalServerError, "unexpected request"
			})
			token := &TokenData{PrivateKey: "old-memory-key", PublicKey: "old-memory-certificate", Subject: "old-subject"}
			input, writer := io.Pipe()
			defer input.Close()
			defer writer.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			result := make(chan error, 1)
			readStarted := make(chan struct{}, 2)
			trackedInput := certificatePromptReader{Reader: input, started: readStarted}
			go func() {
				result <- getCertContext(ctx, settings, token, make(chan logging.LogData, 32), trackedInput, directory)
			}()
			select {
			case <-readStarted:
			case <-time.After(2 * time.Second):
				t.Fatal("reference number prompt did not begin reading")
			}
			if prompt == "authcode" {
				if _, err := io.WriteString(writer, "reference\n"); err != nil {
					t.Fatal(err)
				}
				select {
				case <-readStarted:
				case <-time.After(2 * time.Second):
					t.Fatal("authcode prompt did not begin reading")
				}
			}
			cancel()
			select {
			case err := <-result:
				if !errors.Is(err, context.Canceled) {
					t.Errorf("prompt cancellation returned %v; want context cancellation", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("cancelled certificate prompt did not return")
			}
			assertExistingTestPair(t, directory)
			if token.PrivateKey != "old-memory-key" || token.PublicKey != "old-memory-certificate" || token.Subject != "old-subject" {
				t.Error("prompt cancellation changed the active credentials")
			}
		})
	}
}

type certificatePromptReader struct {
	io.Reader
	started chan<- struct{}
}

func (reader certificatePromptReader) Read(buffer []byte) (int, error) {
	reader.started <- struct{}{}
	return reader.Reader.Read(buffer)
}

func TestGetCertCancellationDoesNotDiscardIssuedCertificate(t *testing.T) {
	signer, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	requestStarted := make(chan struct{})
	releaseResponse := make(chan struct{})
	settings := certificateTestSettings(func(request *http.Request) (int, string) {
		close(requestStarted)
		<-releaseResponse
		if request.Context().Err() != nil {
			return http.StatusInternalServerError, "issuance request was cancelled"
		}
		return http.StatusOK, issuedCertificateHTML(t, request, signer, true)
	})
	directory := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- getCertContext(ctx, settings, nil, make(chan logging.LogData, 32), strings.NewReader("reference\nlocal-authcode\n"), directory)
	}()
	select {
	case <-requestStarted:
	case <-time.After(3 * time.Second):
		close(releaseResponse)
		t.Fatal("certificate issuance request did not begin")
	}
	cancel()
	close(releaseResponse)
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("shutdown discarded the issuance result: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("issued certificate was not installed")
	}
	if _, err := os.Stat(filepath.Join(directory, "channel.cer")); err != nil {
		t.Error("issued certificate was lost during shutdown")
	}
}
