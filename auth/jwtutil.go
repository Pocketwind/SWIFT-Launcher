package auth

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
)

func SimpleJTI() string {
	//완전 랜덤 32바이트 값 불러와서 문자열로 변경
	randomBytes := make([]byte, 32)
	if _, err := rand.Read(randomBytes); err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(randomBytes)
}

func NormalizeToX5C(cert string) (string, error) {
	if !strings.Contains(cert, "-----BEGIN CERTIFICATE-----") {
		return "", fmt.Errorf("public certificate is missing header: -----BEGIN CERTIFICATE-----")
	}
	if !strings.Contains(cert, "-----END CERTIFICATE-----") {
		return "", fmt.Errorf("public certificate is missing footer: -----END CERTIFICATE-----")
	}

	cert = strings.ReplaceAll(cert, "-----BEGIN CERTIFICATE-----", "")
	cert = strings.ReplaceAll(cert, "-----END CERTIFICATE-----", "")
	cert = strings.ReplaceAll(cert, "\n", "")
	cert = strings.ReplaceAll(cert, "\r", "")
	cert = strings.TrimSpace(cert)

	return cert, nil
}

func B64urlWithPaddingFromString(s string) string {
	b64 := base64.URLEncoding.EncodeToString([]byte(s))
	paddingNeeded := (4 - len(b64)%4) % 4
	if paddingNeeded == 0 {
		return b64
	}
	return b64 + strings.Repeat("=", paddingNeeded)
}
