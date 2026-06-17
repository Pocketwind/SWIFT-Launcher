package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"net/url"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func NRSignatureMaker(rawURL string, sub string, body string, privKey string, pubKey string) (string, error) {
	// NRSignature 만들기
	x5cPub, err := NormalizeToX5C(pubKey)
	if err != nil {
		return "", err
	}
	currentTime := time.Now().Unix()

	// url parse: hostname + path + ?query
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	audDynamic := parsedURL.Hostname() + parsedURL.Path
	if parsedURL.RawQuery != "" {
		audDynamic += "?" + parsedURL.RawQuery
	}

	// body digest: sha256(base64url(body with padding as in Python helper)) -> std base64
	b64urlPadded := B64urlWithPaddingFromString(body)
	digestHash := sha256.Sum256([]byte(b64urlPadded))
	digest := base64.StdEncoding.EncodeToString(digestHash[:])

	// jwt payload
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"aud":    audDynamic,
		"sub":    sub,
		"jti":    SimpleJTI(),
		"exp":    jwt.NewNumericDate(time.Unix(currentTime+300, 0)), //5분 뒤 만료
		"iat":    jwt.NewNumericDate(time.Unix(currentTime, 0)),
		"digest": digest,
	})
	token.Header["typ"] = "JWT"
	token.Header["x5c"] = []string{x5cPub}

	privateKey, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(privKey))
	if err != nil {
		return "", err
	}

	signed, err := token.SignedString(privateKey)
	if err != nil {
		return "", err
	}

	return signed, nil
}
