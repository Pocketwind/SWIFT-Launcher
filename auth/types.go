package auth

import "sync"

// 토큰 정보 구조체
type TokenData struct {
	mu               sync.RWMutex `json:"-"`
	AccessToken      string       `json:"access_token"`
	RefreshToken     string       `json:"refresh_token"`
	ExpireTimeString string       `json:"expires_in"` //문자열로 와서 string만 담음
	ExpireTime       int64        //문자열 온거 변환
	TokenType        string       `json:"token_type"`
	TokenPurpose     string
	ConsumerKey      string
	ConsumerSecret   string
	Username         string
	Password         string
	PrivateKey       string
	PublicKey        string
	GrantType        string
	Scope            string
	Subject          string
	Audience         string
}

func (t *TokenData) Lock() {
	t.mu.Lock()
}

func (t *TokenData) Unlock() {
	t.mu.Unlock()
}

func (t *TokenData) RLock() {
	t.mu.RLock()
}

func (t *TokenData) RUnlock() {
	t.mu.RUnlock()
}

// 인증서 요청 Struct
type CertRequest struct {
	Action          string
	ReferenceNumber string
	Authcode        string
	RetrievedAs     string
	Pkcs10Request   string
}
