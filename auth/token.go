package auth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Pocketwind/SWIFT-Launcher/config"
	"github.com/Pocketwind/SWIFT-Launcher/logging"
	jwt "github.com/golang-jwt/jwt/v5"
)

func TokenService(settings *config.Settings, tokenData *TokenData, logCh chan<- logging.LogData, exitCmd <-chan bool) {
	//초기 실행
	if err := Auth(settings, tokenData, logCh); err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Authentication failed: %v", err))
	}

	//ticker 0인지 확인
	if settings.Messaging.TokenTimeout <= 0 {
		logging.Easylog(logCh, "INFO", "Token timeout is set to 0 or negative. Token service will not refresh tokens automatically.")
		<-exitCmd
		logging.Easylog(logCh, "INFO", "Token Service Stopped")
		return
	}
	ticker := time.NewTicker(time.Duration(settings.Messaging.TokenTimeout) * time.Second)
	defer ticker.Stop()
loop:
	for {
		select {
		case <-ticker.C:
			if err := Auth(settings, tokenData, logCh); err != nil {
				logging.Easylog(logCh, "ERROR", fmt.Sprintf("Authentication failed: %v", err))
			}

		case <-exitCmd:
			break loop
		}
	}
	logging.Easylog(logCh, "INFO", "Token Service Stopped")
}

// Auth serializes refresh and only replaces validated credentials.
func Auth(settings *config.Settings, tokenData *TokenData, logCh chan<- logging.LogData) error {
	if settings == nil || settings.Messaging.HttpClient == nil || tokenData == nil {
		return fmt.Errorf("authentication requires settings, HTTP client and token data")
	}
	tokenData.authMu.Lock()
	defer tokenData.authMu.Unlock()
	tokenData.RLock()
	accessToken, expireTime := tokenData.AccessToken, tokenData.ExpireTime
	purpose, refreshToken := tokenData.TokenPurpose, tokenData.RefreshToken
	consumerKey, consumerSecret := tokenData.ConsumerKey, tokenData.ConsumerSecret
	grantType, scope := tokenData.GrantType, tokenData.Scope
	tokenData.RUnlock()
	if accessToken != "" && time.Now().Unix() < expireTime {
		return nil
	}
	if purpose != "messaging" {
		return fmt.Errorf("unsupported token purpose %q", purpose)
	}
	form := url.Values{}
	refreshing := accessToken != "" && refreshToken != ""
	if refreshing {
		form.Set("grant_type", "refresh_token")
		form.Set("refresh_token", refreshToken)
	} else {
		assertion, err := createJWT(tokenData)
		if err != nil {
			return err
		}
		form.Set("grant_type", grantType)
		form.Set("scope", scope)
		form.Set("assertion", assertion)
	}
	req, err := http.NewRequest("POST", settings.Messaging.TokenUrl, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Basic "+getBasicToken(consumerKey, consumerSecret))
	resp, err := settings.Messaging.HttpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request token: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("token request failed: HTTP %d", resp.StatusCode)
	}
	var result struct {
		AccessToken  string          `json:"access_token"`
		RefreshToken string          `json:"refresh_token"`
		TokenType    string          `json:"token_type"`
		ExpiresIn    json.RawMessage `json:"expires_in"`
	}
	const maxResponseSize = 1 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize+1))
	if err != nil {
		return fmt.Errorf("read token response: %w", err)
	}
	if len(body) > maxResponseSize {
		return fmt.Errorf("token response too large")
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("invalid token response JSON")
	}
	lifetimeText := strings.TrimSpace(string(result.ExpiresIn))
	if strings.HasPrefix(lifetimeText, "\"") {
		if err := json.Unmarshal(result.ExpiresIn, &lifetimeText); err != nil {
			return fmt.Errorf("invalid token lifetime")
		}
	}
	lifetime, err := strconv.ParseInt(lifetimeText, 10, 64)
	if err != nil || lifetime <= 0 || lifetime > 365*24*60*60 {
		return fmt.Errorf("invalid token lifetime")
	}
	if strings.TrimSpace(result.AccessToken) == "" || !strings.EqualFold(result.TokenType, "Bearer") {
		return fmt.Errorf("token response missing access token or valid token type")
	}
	margin := int64(60)
	if lifetime <= margin {
		margin = lifetime / 10
	}
	tokenData.Lock()
	tokenData.AccessToken, tokenData.TokenType = result.AccessToken, "Bearer"
	tokenData.ExpireTimeString = lifetimeText
	tokenData.ExpireTime = time.Now().Unix() + lifetime - margin
	if result.RefreshToken != "" || !refreshing {
		tokenData.RefreshToken = result.RefreshToken
	}
	tokenData.Unlock()
	logging.Easylog(logCh, "INFO", "Access token obtained successfully")
	return nil
}

func getBasicToken(consumerKey, consumerSecret string) string {
	return base64.StdEncoding.EncodeToString([]byte(consumerKey + ":" + consumerSecret))
}

func createJWT(tokenData *TokenData) (string, error) {
	tokenData.RLock()
	consumerKey := tokenData.ConsumerKey
	audience := tokenData.Audience
	subject := tokenData.Subject
	publicKey := tokenData.PublicKey
	privateKeyPem := tokenData.PrivateKey
	tokenData.RUnlock()

	currentTime := time.Now().Unix()
	expireTime := currentTime + 900
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss": consumerKey,
		"aud": audience,
		"sub": subject,
		"exp": expireTime,
		"iat": currentTime,
		"jti": SimpleJTI(),
	})
	token.Header["typ"] = "JWT"
	token.Header["alg"] = "RS256"
	x5c, err := NormalizeToX5C(publicKey)
	if err != nil {
		return "", fmt.Errorf("failed to normalize x5c: %v", err)
	}
	token.Header["x5c"] = []string{x5c}
	privateKey, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(privateKeyPem))
	if err != nil {
		return "", fmt.Errorf("failed to parse private key: %v", err)
	}
	tokenString, err := token.SignedString(privateKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %v", err)
	}
	return tokenString, nil
}

func RevokeToken(settings *config.Settings, tokenData *TokenData, logCh chan<- logging.LogData) {
	tokenData.RLock()
	accessToken := tokenData.AccessToken
	tokenData.RUnlock()
	if accessToken == "" {
		logging.Easylog(logCh, "INFO", "No token data available. Skipping token revocation.")
		return
	}
	revokeUrl := settings.Messaging.RevokeTokenUrl
	tokenData.RLock()
	consumerKey := tokenData.ConsumerKey
	consumerSecret := tokenData.ConsumerSecret
	tokenData.RUnlock()
	basicToken := getBasicToken(consumerKey, consumerSecret)
	form := url.Values{}
	form.Set("token", accessToken)

	req, err := http.NewRequest("POST", revokeUrl, strings.NewReader(form.Encode()))
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error creating request: %v", err))
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", fmt.Sprintf("Basic %v", basicToken))

	//real request
	client := settings.Messaging.HttpClient
	resp, err := client.Do(req)
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error making request: %v", err))
		return
	}
	defer resp.Body.Close()

	logging.Easylog(logCh, "INFO", fmt.Sprintf("Token revocation response status: %v", resp.Status))
}
