package auth

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/Pocketwind/SWIFT-Launcher/config"
	"github.com/Pocketwind/SWIFT-Launcher/logging"
	"github.com/antchfx/htmlquery"
)

func GetCert(settings *config.Settings, tokenData *TokenData, logCh chan<- logging.LogData) error {
	return GetCertWithInput(settings, tokenData, logCh, bufio.NewReader(os.Stdin))
}

// GetCertWithInput shares the caller's console reader during certificate renewal.
func GetCertWithInput(settings *config.Settings, tokenData *TokenData, logCh chan<- logging.LogData, reader *bufio.Reader) error {
	return GetCertWithInputContext(context.Background(), settings, tokenData, logCh, reader)
}

// GetCertWithInputContext allows shutdown while a certificate prompt is waiting.
// After cancellation the caller must not reuse reader: its blocked input read
// finishes only when input arrives. Once sent, the HTTP request uses its normal
// timeout so an issued certificate is not discarded because of shutdown.
func GetCertWithInputContext(ctx context.Context, settings *config.Settings, tokenData *TokenData, logCh chan<- logging.LogData, reader *bufio.Reader) error {
	if reader == nil {
		return fmt.Errorf("certificate issuance requires console input")
	}
	return getCertContext(ctx, settings, tokenData, logCh, reader, "pem")
}

func getCert(settings *config.Settings, tokenData *TokenData, logCh chan<- logging.LogData, input io.Reader, certDir string) error {
	return getCertContext(context.Background(), settings, tokenData, logCh, input, certDir)
}

func getCertContext(ctx context.Context, settings *config.Settings, tokenData *TokenData, logCh chan<- logging.LogData, input io.Reader, certDir string) error {
	if ctx == nil || settings == nil || settings.Messaging.HttpClient == nil || input == nil {
		return fmt.Errorf("certificate issuance requires settings, HTTP client and console input")
	}
	var certRequest CertRequest
	reader, buffered := input.(*bufio.Reader)
	if !buffered {
		reader = bufio.NewReader(input)
	}
	fmt.Print("Reference Number: ")
	line, err := readCertificateLine(ctx, reader)
	if err != nil && err != io.EOF {
		return fmt.Errorf("read reference number: %w", err)
	}
	certRequest.ReferenceNumber = strings.TrimSpace(line)

	fmt.Print("Authcode: ")
	line, err = readCertificateLine(ctx, reader)
	if err != nil && err != io.EOF {
		return fmt.Errorf("read authcode: %w", err)
	}
	certRequest.Authcode = strings.TrimSpace(line)
	if certRequest.ReferenceNumber == "" || certRequest.Authcode == "" {
		return fmt.Errorf("reference number and authcode are required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("failed to generate private key: %w", err)
	}

	privateKeyBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})

	csrTemplate := &x509.CertificateRequest{
		Subject: pkix.Name{
			Organization: []string{"swift"},
			CommonName:   certRequest.ReferenceNumber,
		},
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, csrTemplate, privateKey)
	if err != nil {
		return fmt.Errorf("failed to create CSR: %w", err)
	}

	csrBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})

	//고정값
	certRequest.Action = "getServerCert"
	certRequest.RetrievedAs = "rawDER"

	certRequest.Pkcs10Request = string(csrBytes)

	//인증서 요청
	form := url.Values{}
	form.Set("action", certRequest.Action)
	form.Set("reference_number", certRequest.ReferenceNumber)
	form.Set("authcode", certRequest.Authcode)
	form.Set("retrievedAs", certRequest.RetrievedAs)
	form.Set("pkcs10Request", certRequest.Pkcs10Request)
	requrl := "https://wbcl02.swiftnet.sipn.swift.com:49171/cda-cgi/clientcgi"
	req, err := http.NewRequest("POST", requrl, strings.NewReader(form.Encode()))
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error creating request: %v", err))
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := settings.Messaging.HttpClient
	resp, err := client.Do(req)
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error making request: %v", err))
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error response from server: %v", resp.Status))
		return fmt.Errorf("error response from server: %v", resp.Status)
	}

	const maxCertResponseSize = 1 << 20
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, maxCertResponseSize+1))
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error reading response: %v", err))
		return err
	}
	if len(responseBody) > maxCertResponseSize {
		return fmt.Errorf("certificate response too large")
	}

	//파싱
	doc, err := htmlquery.Parse(strings.NewReader(string(responseBody)))
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error parsing response: %v", err))
		return err
	}
	certNode := htmlquery.FindOne(doc, "//font")
	if certNode == nil {
		logging.Easylog(logCh, "ERROR", "Error finding certificate node in response")
		return fmt.Errorf("error finding certificate node")
	}
	certData := htmlquery.InnerText(certNode)

	//파일로 저장
	certData = strings.ReplaceAll(certData, "\t", "")
	certData = strings.ReplaceAll(certData, "\n\n", "\n")
	certData = strings.ReplaceAll(certData, "    ", "")
	certData = strings.TrimSpace(certData)
	block, remaining := pem.Decode([]byte(certData))
	if block == nil || block.Type != "CERTIFICATE" || len(strings.TrimSpace(string(remaining))) != 0 {
		return fmt.Errorf("response does not contain a single PEM certificate")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return fmt.Errorf("parse issued certificate: %w", err)
	}
	publicKey, ok := certificate.PublicKey.(*rsa.PublicKey)
	if !ok || publicKey.E != privateKey.PublicKey.E || publicKey.N.Cmp(privateKey.PublicKey.N) != 0 {
		return fmt.Errorf("issued certificate does not match the new private key")
	}
	publicKeyBytes := pem.EncodeToMemory(block)
	subject, err := GetCertDN(certData)
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Failed to extract DN from certificate: %v", err))
		return err
	}
	if tokenData != nil {
		tokenData.authMu.Lock()
	}
	if err := installCertificatePair(certDir, privateKeyBytes, publicKeyBytes, os.Rename); err != nil {
		if tokenData != nil {
			tokenData.authMu.Unlock()
		}
		return fmt.Errorf("install certificate and private key: %w", err)
	}

	//nil 받을때(초기실행) 구분
	if tokenData != nil {
		tokenData.Lock()
		tokenData.Subject = subject
		tokenData.PublicKey = string(publicKeyBytes)
		tokenData.PrivateKey = string(privateKeyBytes)
		tokenData.Unlock()
		tokenData.authMu.Unlock()
	} else {
		settings.Messaging.Subject = subject
	}
	// The CSR is diagnostic; its failure does not invalidate the installed pair.
	if err := os.WriteFile(filepath.Join(certDir, "channel.csr"), csrBytes, 0644); err != nil {
		logging.Easylog(logCh, "WARN", fmt.Sprintf("Certificate installed, but CSR could not be saved: %v", err))
	}

	//완료
	certPath := filepath.Join(certDir, "channel.cer")
	logging.Easylog(logCh, "INFO", fmt.Sprintf("Certificate saved successfully: %s", certPath))
	fmt.Println("------------------------------------------")
	fmt.Printf("Certificate saved successfully: %s\n", certPath)
	fmt.Println("------------------------------------------")

	return nil
}

func readCertificateLine(ctx context.Context, reader *bufio.Reader) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if ctx.Done() == nil {
		return reader.ReadString('\n')
	}
	type inputResult struct {
		line string
		err  error
	}
	result := make(chan inputResult, 1)
	go func() {
		line, err := reader.ReadString('\n')
		result <- inputResult{line: line, err: err}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case read := <-result:
		if err := ctx.Err(); err != nil {
			return "", err
		}
		return read.line, read.err
	}
}

// installCertificatePair stages both files before moving the old pair aside.
// Rename failures restore the old pair; a process crash between file renames
// still requires recovery from the backup files.
func installCertificatePair(dir string, key, certificate []byte, rename func(string, string) error) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	type installation struct {
		path, temporary, backup string
		backedUp, installed     bool
	}
	files := []*installation{
		{path: filepath.Join(dir, "channel.key")},
		{path: filepath.Join(dir, "channel.cer")},
	}
	defer func() {
		for _, file := range files {
			if file.temporary != "" {
				os.Remove(file.temporary)
			}
		}
	}()
	for i, data := range [][]byte{key, certificate} {
		mode := os.FileMode(0644)
		if i == 0 {
			mode = 0600
		}
		name, err := stageCertificateFile(dir, filepath.Base(files[i].path)+".new-", data, mode)
		if err != nil {
			return err
		}
		files[i].temporary = name
		info, err := os.Lstat(files[i].path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("certificate target is not a regular file: %s", files[i].path)
		}
		backup, err := os.CreateTemp(dir, filepath.Base(files[i].path)+".backup-")
		if err != nil {
			return err
		}
		files[i].backup = backup.Name()
		if err := backup.Close(); err != nil {
			os.Remove(backup.Name())
			return err
		}
		if err := os.Remove(backup.Name()); err != nil {
			return err
		}
	}
	rollback := func(cause error) error {
		for i := len(files) - 1; i >= 0; i-- {
			file := files[i]
			if file.installed {
				if err := os.Remove(file.path); err != nil && !os.IsNotExist(err) {
					cause = errors.Join(cause, fmt.Errorf("remove newly installed %s: %w", file.path, err))
				}
			}
			if file.backedUp {
				if err := rename(file.backup, file.path); err != nil {
					cause = errors.Join(cause, fmt.Errorf("restore %s from backup %s: %w", file.path, file.backup, err))
				}
			}
		}
		return cause
	}
	for _, file := range files {
		if file.backup != "" {
			if err := rename(file.path, file.backup); err != nil {
				return rollback(err)
			}
			file.backedUp = true
		}
	}
	for _, file := range files {
		if err := rename(file.temporary, file.path); err != nil {
			return rollback(err)
		}
		file.installed = true
	}
	for _, file := range files {
		if file.backedUp {
			os.Remove(file.backup)
		}
	}
	return nil
}

func stageCertificateFile(dir, prefix string, data []byte, mode os.FileMode) (string, error) {
	file, err := os.CreateTemp(dir, prefix)
	if err != nil {
		return "", err
	}
	name := file.Name()
	complete := false
	defer func() {
		file.Close()
		if !complete {
			os.Remove(name)
		}
	}()
	if err := file.Chmod(mode); err != nil {
		return "", err
	}
	if _, err := file.Write(data); err != nil {
		return "", err
	}
	if err := file.Sync(); err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	complete = true
	return name, nil
}

func GetCertDN(pemString string) (string, error) {
	block, _ := pem.Decode([]byte(pemString))
	if block == nil {
		return "", fmt.Errorf("failed to parse PEM block")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("failed to parse certificate: %v", err)
	}

	//Subject 빌더
	var subjectBuilder strings.Builder
	for i := len(cert.Subject.Names) - 1; i >= 0; i-- {
		name := cert.Subject.Names[i]
		switch name.Type.String() {
		case "2.5.4.3":
			subjectBuilder.WriteString(fmt.Sprintf("CN=%s, ", name.Value))
		case "2.5.4.10":
			subjectBuilder.WriteString(fmt.Sprintf("O=%s, ", name.Value))
		}
	}
	subject := strings.TrimSuffix(subjectBuilder.String(), ", ")
	subject = strings.ReplaceAll(subject, " ", "")
	subject = strings.ToLower(subject)

	return subject, nil
}

func GetCertInfo(pemString string) {
	block, _ := pem.Decode([]byte(pemString))
	if block == nil {
		fmt.Println("Failed to parse PEM block")
		return
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		fmt.Printf("Failed to parse certificate: %v\n", err)
		return
	}
	fmt.Println("------------------------------------------")
	fmt.Println("Certificate Information:")
	fmt.Printf("Expiry: %s\n", cert.NotAfter)
	dn, err := GetCertDN(pemString)
	if err != nil {
		fmt.Printf("Failed to get DN: %v\n", err)
		return
	}
	fmt.Printf("DN: %s\n", dn)
	fmt.Println("------------------------------------------")
}
