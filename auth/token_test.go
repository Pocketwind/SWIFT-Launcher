package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Pocketwind/SWIFT-Launcher/config"
	"github.com/Pocketwind/SWIFT-Launcher/logging"
)

func expiredTestToken() *TokenData {
	return &TokenData{
		AccessToken: "previous-access-secret", RefreshToken: "previous-refresh-secret",
		ExpireTimeString: "1800", ExpireTime: time.Now().Unix() - 10,
		TokenType: "Bearer", TokenPurpose: "messaging",
		ConsumerKey: "test-consumer", ConsumerSecret: "test-consumer-secret",
	}
}

func tokenTestSettings(server *httptest.Server) *config.Settings {
	client := server.Client()
	client.Timeout = 3 * time.Second
	return &config.Settings{Messaging: config.Messaging{TokenUrl: server.URL, HttpClient: client}}
}

func assertNoTokenSecrets(t *testing.T, logs <-chan logging.LogData, err error) {
	t.Helper()
	var messages strings.Builder
	if err != nil {
		messages.WriteString(err.Error())
	}
	for len(logs) > 0 {
		messages.WriteString((<-logs).Text)
	}
	for _, secret := range []string{
		"previous-access-secret", "previous-refresh-secret",
		"issued-access-secret", "issued-refresh-secret", "test-consumer-secret",
	} {
		if strings.Contains(messages.String(), secret) {
			t.Errorf("authentication logs or error disclose %q", secret)
		}
	}
}

func TestAuthAcceptsStringAndNumericExpiry(t *testing.T) {
	for _, tc := range []struct {
		name, expiry string
		ttl          int64
	}{
		{name: "string", expiry: `"3600"`, ttl: 3600},
		{name: "number", expiry: `3600`, ttl: 3600},
		{name: "short lifetime", expiry: `30`, ttl: 30},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				if r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("refresh_token") != "previous-refresh-secret" {
					t.Errorf("unexpected refresh form: %v", r.Form)
				}
				w.Header().Set("Content-Type", "application/json")
				io.WriteString(w, `{"access_token":"issued-access-secret","refresh_token":"issued-refresh-secret","token_type":"Bearer","expires_in":`+tc.expiry+`}`)
			}))
			defer server.Close()
			token := expiredTestToken()
			logs := make(chan logging.LogData, 32)
			before := time.Now().Unix()
			err := Auth(tokenTestSettings(server), token, logs)
			if err != nil {
				t.Fatalf("Auth failed: %v", err)
			}
			if token.AccessToken != "issued-access-secret" || token.RefreshToken != "issued-refresh-secret" {
				t.Error("successful authentication did not replace both tokens")
			}
			if token.ExpireTime <= before || token.ExpireTime > time.Now().Unix()+tc.ttl {
				t.Errorf("expiry %d is outside usable token lifetime", token.ExpireTime)
			}
			assertNoTokenSecrets(t, logs, err)
		})
	}
}

func TestAuthFailurePreservesExistingToken(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{name: "HTTP failure", status: http.StatusUnauthorized, body: `{"access_token":"issued-access-secret","refresh_token":"issued-refresh-secret","token_type":"Bearer","expires_in":"3600"}`},
		{name: "malformed JSON", status: http.StatusOK, body: `{"access_token":"issued-access-secret","expires_in":`},
		{name: "invalid expiry", status: http.StatusOK, body: `{"access_token":"issued-access-secret","token_type":"Bearer","expires_in":"not-a-number"}`},
		{name: "zero expiry", status: http.StatusOK, body: `{"access_token":"issued-access-secret","token_type":"Bearer","expires_in":0}`},
		{name: "negative expiry", status: http.StatusOK, body: `{"access_token":"issued-access-secret","token_type":"Bearer","expires_in":-10}`},
		{name: "fractional expiry", status: http.StatusOK, body: `{"access_token":"issued-access-secret","token_type":"Bearer","expires_in":1.5}`},
		{name: "missing expiry", status: http.StatusOK, body: `{"access_token":"issued-access-secret","token_type":"Bearer"}`},
		{name: "missing access token", status: http.StatusOK, body: `{"refresh_token":"issued-refresh-secret","token_type":"Bearer","expires_in":"3600"}`},
		{name: "empty response", status: http.StatusOK, body: ``},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			}))
			defer server.Close()
			token := expiredTestToken()
			previousExpiry := token.ExpireTime
			logs := make(chan logging.LogData, 32)
			err := Auth(tokenTestSettings(server), token, logs)
			if err == nil {
				t.Error("invalid token response was accepted")
			}
			if token.AccessToken != "previous-access-secret" || token.RefreshToken != "previous-refresh-secret" || token.ExpireTime != previousExpiry || token.ExpireTimeString != "1800" || token.TokenType != "Bearer" {
				t.Error("failed authentication modified the existing token state")
			}
			assertNoTokenSecrets(t, logs, err)
		})
	}
}

func TestAuthRetainsRefreshTokenWhenResponseOmitsIt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"access_token":"issued-access-secret","token_type":"Bearer","expires_in":"3600"}`)
	}))
	defer server.Close()
	token := expiredTestToken()
	logs := make(chan logging.LogData, 32)
	if err := Auth(tokenTestSettings(server), token, logs); err != nil {
		t.Fatal(err)
	}
	if token.RefreshToken != "previous-refresh-secret" {
		t.Error("refresh response discarded the existing refresh token")
	}
	assertNoTokenSecrets(t, logs, nil)
}

func TestAuthSerializesConcurrentRefresh(t *testing.T) {
	var requests atomic.Int32
	firstRequest := make(chan struct{}, 1)
	releaseResponse := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		select {
		case firstRequest <- struct{}{}:
		default:
		}
		<-releaseResponse
		io.WriteString(w, `{"access_token":"issued-access-secret","refresh_token":"issued-refresh-secret","token_type":"Bearer","expires_in":"3600"}`)
	}))
	defer server.Close()
	token := expiredTestToken()
	settings := tokenTestSettings(server)
	logs := make(chan logging.LogData, 256)
	const workers = 24
	start := make(chan struct{})
	results := make(chan error, workers)
	var ready sync.WaitGroup
	ready.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			ready.Done()
			<-start
			results <- Auth(settings, token, logs)
		}()
	}
	ready.Wait()
	close(start)
	select {
	case <-firstRequest:
		// Keep the first response in flight while the other callers enter Auth.
		time.Sleep(50 * time.Millisecond)
	case <-time.After(3 * time.Second):
		close(releaseResponse)
		t.Fatal("authentication request did not reach the test server")
	}
	close(releaseResponse)
	for i := 0; i < workers; i++ {
		if err := <-results; err != nil {
			t.Errorf("concurrent Auth failed: %v", err)
		}
	}
	if got := requests.Load(); got != 1 {
		t.Errorf("concurrent callers performed %d refreshes; want 1", got)
	}
	assertNoTokenSecrets(t, logs, nil)
}

func TestAuthDoesNotRequestUnexpiredToken(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	token := expiredTestToken()
	token.ExpireTime = time.Now().Unix() + 1800
	if err := Auth(tokenTestSettings(server), token, make(chan logging.LogData, 32)); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 0 {
		t.Error("valid cached token triggered a network request")
	}
}

type tokenTestRoundTripper func(*http.Request) (*http.Response, error)

func (f tokenTestRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

type failedTokenBody struct{}

func (failedTokenBody) Read([]byte) (int, error) {
	return 0, errors.New("simulated response read failure")
}
func (failedTokenBody) Close() error { return nil }

func TestAuthBodyReadFailurePreservesExistingToken(t *testing.T) {
	settings := &config.Settings{Messaging: config.Messaging{
		TokenUrl: "http://token.test.invalid",
		HttpClient: &http.Client{Transport: tokenTestRoundTripper(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: failedTokenBody{}, Header: make(http.Header)}, nil
		})},
	}}
	token := expiredTestToken()
	previousExpiry := token.ExpireTime
	logs := make(chan logging.LogData, 32)
	err := Auth(settings, token, logs)
	if err == nil {
		t.Error("response read failure was accepted")
	}
	if token.AccessToken != "previous-access-secret" || token.RefreshToken != "previous-refresh-secret" || token.ExpireTime != previousExpiry {
		t.Error("response read failure modified token state")
	}
	assertNoTokenSecrets(t, logs, err)
}

func TestAuthIssuesJWTWhenRefreshTokenIsMissing(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	certTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "local-token-test"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, certTemplate, certTemplate, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	token := expiredTestToken()
	token.RefreshToken = ""
	token.GrantType = "urn:ietf:params:oauth:grant-type:jwt-bearer"
	token.Scope = "test-scope"
	token.Audience = "test-audience"
	token.Subject = "test-subject"
	token.PrivateKey = string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	token.PublicKey = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.Form.Get("grant_type") != token.GrantType || r.Form.Get("scope") != token.Scope || len(strings.Split(r.Form.Get("assertion"), ".")) != 3 {
			t.Error("missing refresh token did not use a signed JWT assertion")
		}
		if r.Header.Get("Authorization") != "Basic "+getBasicToken("test-consumer", "test-consumer-secret") {
			t.Error("JWT issuance omitted consumer authentication")
		}
		io.WriteString(w, `{"access_token":"issued-access-secret","refresh_token":"issued-refresh-secret","token_type":"Bearer","expires_in":"3600"}`)
	}))
	defer server.Close()
	logs := make(chan logging.LogData, 32)
	err = Auth(tokenTestSettings(server), token, logs)
	if err != nil {
		t.Fatalf("JWT issuance failed: %v", err)
	}
	if token.AccessToken != "issued-access-secret" || token.RefreshToken != "issued-refresh-secret" {
		t.Error("JWT issuance did not populate token state")
	}
	assertNoTokenSecrets(t, logs, err)
}
