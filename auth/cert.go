package auth

import (
	"bufio"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/Pocketwind/SWIFT-Launcher/config"
	"github.com/Pocketwind/SWIFT-Launcher/logging"
	"github.com/antchfx/htmlquery"
)

func GetCert(settings *config.Settings, tokenData *TokenData, logCh chan<- logging.LogData) error {
	var certRequest CertRequest
	var filename string

	//데이터 입력
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Reference Number: ")
	input, _ := reader.ReadString('\n')
	certRequest.ReferenceNumber = strings.TrimSpace(input)

	fmt.Print("Authcode: ")
	input, _ = reader.ReadString('\n')
	certRequest.Authcode = strings.TrimSpace(input)

	filename = "channel"

	//키 및 CSR 생성
	if err := os.MkdirAll("pem", 0755); err != nil {
		return err
	}

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("failed to generate private key: %w", err)
	}

	keyFile, err := os.Create("pem/" + filename + ".key")
	if err != nil {
		return fmt.Errorf("failed to create key file: %w", err)
	}
	if err := pem.Encode(keyFile, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)}); err != nil {
		keyFile.Close()
		return fmt.Errorf("failed to write key file: %w", err)
	}
	keyFile.Close()

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

	csrFile, err := os.Create("pem/" + filename + ".csr")
	if err != nil {
		return fmt.Errorf("failed to create CSR file: %w", err)
	}
	if err := pem.Encode(csrFile, &pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER}); err != nil {
		csrFile.Close()
		return fmt.Errorf("failed to write CSR file: %w", err)
	}
	csrFile.Close()

	//고정값
	certRequest.Action = "getServerCert"
	certRequest.RetrievedAs = "rawDER"

	//csr 읽기
	csrBytes, err := os.ReadFile("pem/" + filename + ".csr")
	if err != nil {
		return err
	}
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

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error reading response: %v", err))
		return err
	}

	//파싱
	doc, err := htmlquery.Parse(strings.NewReader(string(responseBody)))
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error parsing response: %v", err))
		return err
	}
	certNode := htmlquery.FindOne(doc, "//font")
	if certNode == nil {
		bodyPreview := strings.TrimSpace(string(responseBody))
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error finding certificate node. Response preview: %s", bodyPreview))
		return fmt.Errorf("error finding certificate node")
	}
	certData := htmlquery.InnerText(certNode)

	//파일로 저장
	certData = strings.ReplaceAll(certData, "\t", "")
	certData = strings.ReplaceAll(certData, "\n\n", "\n")
	certData = strings.ReplaceAll(certData, "    ", "")
	certData = strings.TrimSpace(certData)
	certData = strings.ReplaceAll(certData, "\n", "\r\n")
	err = os.WriteFile("pem/"+filename+".cer", []byte(certData), 0644)
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error writing certificate file: %v", err))
		return err
	}

	//인증서 정보 교체
	privateKeyBytes, err := os.ReadFile("pem/" + filename + ".key")
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error reading private key file: %v", err))
		return err
	}
	publicKeyBytes, err := os.ReadFile("pem/" + filename + ".cer")
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error reading public key file: %v", err))
		return err
	}
	settings.Messaging.Subject, err = GetCertDN(certData)
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Failed to extract DN from certificate: %v", err))
		return err
	}

	//nil 받을때(초기실행) 구분
	if tokenData != nil {
		tokenData.Lock()
		tokenData.Subject = settings.Messaging.Subject
		tokenData.PublicKey = string(publicKeyBytes)
		tokenData.PrivateKey = string(privateKeyBytes)
		tokenData.Unlock()
	}

	//완료
	logging.Easylog(logCh, "INFO", fmt.Sprintf("Certificate saved successfully: pem/%s.cer", filename))
	fmt.Println("------------------------------------------")
	fmt.Printf("Certificate saved successfully: pem/%s.cer\n", filename)
	fmt.Println("------------------------------------------")

	return nil
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
