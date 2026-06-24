package httpclient

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/Pocketwind/SWIFT-Launcher/config"
)

func appendCertsFromFile(pool *x509.CertPool, filePath string) error {
	certData, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	if ok := pool.AppendCertsFromPEM(certData); ok {
		return nil
	}

	if block, _ := pem.Decode(certData); block != nil {
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return err
		}
		pool.AddCert(cert)
		return nil
	}

	cert, err := x509.ParseCertificate(certData)
	if err != nil {
		return err
	}
	pool.AddCert(cert)
	return nil
}

func MakeHTTPClient(settings *config.Settings) (*http.Client, error) {
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
		if err := appendCertsFromFile(rootCAs, caCertPath); err != nil {
			fmt.Printf("WARN: failed to load custom CA cert from %s: %v\n", caCertPath, err)
		}
	}

	fmt.Printf("HTTP Client created with timeout: %s and CA certs from: %s\n", timeout, settings.CACertPath)
	fmt.Printf("Loaded %d CA certificates into trust pool\n", len(rootCAs.Subjects()))

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: rootCAs},
	}

	//프록시 없으면(url == "")
	if settings.Messaging.Proxy == "" {
		return &http.Client{Timeout: timeout, Transport: transport}, nil
	}

	//프록시 있으면
	proxyURL, err := url.Parse(settings.Messaging.Proxy)
	if err != nil {
		return nil, err
	}

	transport.Proxy = http.ProxyURL(proxyURL)
	client := &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}
	return client, nil
}
