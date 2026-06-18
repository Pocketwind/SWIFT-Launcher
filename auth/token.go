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
	Auth(settings, tokenData, logCh)

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
			Auth(settings, tokenData, logCh)

		case <-exitCmd:
			break loop
		}
	}
	logging.Easylog(logCh, "INFO", "Token Service Stopped")
}

func Auth(settings *config.Settings, tokenData *TokenData, logCh chan<- logging.LogData) {

	//access token 발급했는지 체크
	tokenData.RLock()
	accessToken := tokenData.AccessToken
	expireTime := tokenData.ExpireTime
	tokenPurpose := tokenData.TokenPurpose
	tokenData.RUnlock()

	currentTime := time.Now().Unix() // + 1730 //토큰 만료 테스트용
	needsIssue := accessToken == ""
	needsRefresh := !needsIssue && currentTime >= expireTime
	if !needsIssue && !needsRefresh {
		return
	}

	if needsIssue {
		logging.Easylog(logCh, "INFO", "No access token found. Requesting new token...")
		switch tokenPurpose {
		case "ref":
			//getPasswordAccessToken(settings, tokenData, logCh)
		case "messaging":
			getJWTAccessToken(settings, tokenData, logCh)
		}
	} else {
		logging.Easylog(logCh, "INFO", "Access token is expired. Refreshing token...")
		switch tokenPurpose {
		case "ref":
			//refreshPasswordAccessToken(settings, tokenData, logCh)
		case "messaging":
			refreshJWTAccessToken(settings, tokenData, logCh)
		}
	}
}

func getBasicToken(consumerKey, consumerSecret string) string {
	basicToken := fmt.Sprintf("%v:%v", consumerKey, consumerSecret)
	return base64.StdEncoding.EncodeToString([]byte(basicToken))
}

func getJWTAccessToken(settings *config.Settings, tokenData *TokenData, logCh chan<- logging.LogData) {
	tokenData.RLock()
	consumerKey := tokenData.ConsumerKey
	consumerSecret := tokenData.ConsumerSecret
	grantType := tokenData.GrantType
	scope := tokenData.Scope
	tokenData.RUnlock()

	//JWT 토큰 생성
	jwtToken, err := createJWT(tokenData)
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Failed to create JWT: %v", err))
		return
	}
	//basic token
	basicToken := getBasicToken(consumerKey, consumerSecret)

	form := url.Values{}
	form.Set("grant_type", grantType)
	form.Set("scope", scope)
	form.Set("assertion", jwtToken)

	//request 만들기
	tokenUrl := settings.Messaging.TokenUrl
	req, err := http.NewRequest("POST", tokenUrl, strings.NewReader(form.Encode()))
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
	tokenResponse, _ := io.ReadAll(resp.Body)

	//token 추출 후 tokenData로 언마셜
	tokenData.Lock()
	json.Unmarshal([]byte(tokenResponse), tokenData)

	//expireTime 계산
	expireTime, _ := strconv.ParseInt(tokenData.ExpireTimeString, 10, 64)
	tokenData.ExpireTime = time.Now().Unix() + expireTime - 60 //만료 1분 전에 갱신
	tokenData.Unlock()

	if tokenData.AccessToken == "" {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Failed to obtain access token. Response: %s", string(tokenResponse)))
	}

	//토큰 발급 성공
	tokenData.RLock()
	logging.Easylog(logCh, "INFO", fmt.Sprintf("Token: %s", tokenData.AccessToken))
	tokenData.RUnlock()
}

func refreshJWTAccessToken(settings *config.Settings, tokenData *TokenData, logCh chan<- logging.LogData) {
	tokenData.RLock()
	consumerKey := tokenData.ConsumerKey
	consumerSecret := tokenData.ConsumerSecret
	refreshToken := tokenData.RefreshToken
	tokenData.RUnlock()
	basicToken := getBasicToken(consumerKey, consumerSecret)

	//body(form)
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)

	//request 만들기
	tokenUrl := settings.Messaging.TokenUrl
	req, err := http.NewRequest("POST", tokenUrl, strings.NewReader(form.Encode()))
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error creating request: %v", err))
		return
	}

	//헤더
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
	tokenResponse, _ := io.ReadAll(resp.Body)
	tokenData.Lock()
	json.Unmarshal([]byte(tokenResponse), tokenData)

	//expireTime 계산
	expireTime, _ := strconv.ParseInt(tokenData.ExpireTimeString, 10, 64)
	tokenData.ExpireTime = time.Now().Unix() + expireTime - 60 //만료 1분 전에 갱신
	tokenData.Unlock()

	//토큰 갱신 성공
	tokenData.RLock()
	logging.Easylog(logCh, "INFO", fmt.Sprintf("Token refreshed: %s", tokenData.AccessToken))
	tokenData.RUnlock()
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
	if tokenData.AccessToken == "" {
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
	form.Set("token", tokenData.AccessToken)

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
