package httpclient

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/Pocketwind/SWIFT-Launcher/config"
)

func MakeHTTPClient(settings *config.Settings) *http.Client {
	//기본 30초
	timeout := 30 * time.Second
	if settings.Messaging.RequestTimeout > 0 {
		timeout = time.Duration(settings.Messaging.RequestTimeout) * time.Second
	}

	// OS 루트 인증서 + settings.json의 cacertPath(선택)를 병합
	rootCAs, err := x509.SystemCertPool()
	if err != nil || rootCAs == nil {
		rootCAs = x509.NewCertPool()
	}

	if caCertPath := strings.TrimSpace(settings.CACertPath); caCertPath != "" {
		if caPEM, err := os.ReadFile(caCertPath); err == nil {
			rootCAs.AppendCertsFromPEM(caPEM)
		}
	}

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: rootCAs},
	}

	//프록시 없으면(url == "")
	if settings.Messaging.Proxy == "" {
		return &http.Client{Timeout: timeout, Transport: transport}
	}

	//프록시 있으면
	proxyURL, err := url.Parse(settings.Messaging.Proxy)
	if err != nil {
		return &http.Client{Timeout: timeout, Transport: transport}
	}

	transport.Proxy = http.ProxyURL(proxyURL)
	client := &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}
	return client
}
