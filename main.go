package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/Pocketwind/SWIFT-Launcher/auth"
	"github.com/Pocketwind/SWIFT-Launcher/config"
	"github.com/Pocketwind/SWIFT-Launcher/fsutil"
	"github.com/Pocketwind/SWIFT-Launcher/httpclient"
	"github.com/Pocketwind/SWIFT-Launcher/logging"
	"github.com/Pocketwind/SWIFT-Launcher/useragent"
)

func main() {
	//logger 채널
	logCh := make(chan logging.LogData, 10000)
	logging.Easylog(logCh, "INFO", "Application started")
	//초기화
	var wg sync.WaitGroup
	exitCmd := make(chan bool)
	var stopOnce sync.Once
	var shutdownOnce sync.Once
	stopAll := func(reason string) {
		stopOnce.Do(func() {
			logging.Easylog(logCh, "INFO", reason)
			close(exitCmd)
		})
	}
	doneLogger := make(chan struct{})
	startLoggerService(logCh, exitCmd, doneLogger)

	//settings 로드
	settings, err := config.LoadSettings("settings.json")
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Failed to load settings: %v", err))
		fmt.Printf("ERROR: Failed to load settings: %v\n", err)
		return
	}
	logging.Easylog(logCh, "INFO", "Settings loaded successfully")

	//HTTP 클라이언트 생성
	settings.Messaging.HttpClient = httpclient.MakeHTTPClient(settings)

	//파트너 파일 로드
	partners, err := config.LoadPartners(settings.Messaging.PartnerFilePath)
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Failed to load partner file: %v", err))
		fmt.Printf("ERROR: Failed to load partner file: %v\n", err)
		return
	}
	logging.Easylog(logCh, "INFO", "Partner file loaded successfully")

	//채널 인증서 체크(초기실행 체크)
	_, errCert := os.Stat("pem/channel.cer")
	_, errKey := os.Stat("pem/channel.key")
	if os.IsNotExist(errCert) || os.IsNotExist(errKey) {
		logging.Easylog(logCh, "INFO", "No channel certificate found. Starting certificate setup...")
		fmt.Println("No channel certificate found. Starting certificate setup...")
		err := auth.GetCert(settings, nil, logCh)
		if err != nil {
			logging.Easylog(logCh, "ERROR", fmt.Sprintf("Failed to get certificate: %v", err))
			fmt.Printf("ERROR: Failed to get certificate: %v\n", err)
			return
		}
	}

	//키 파일 로드
	publicKeyBytes, err := os.ReadFile("pem/channel.cer")
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Failed to read public key file: %v", err))
		fmt.Printf("ERROR: Failed to read public key file: %v\n", err)
		return
	}
	privateKeyBytes, err := os.ReadFile("pem/channel.key")
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Failed to read private key file: %v", err))
		fmt.Printf("ERROR: Failed to read private key file: %v\n", err)
		return
	}
	publicKey := string(publicKeyBytes)
	privateKey := string(privateKeyBytes)

	//DN 추출
	settings.Messaging.Subject, err = auth.GetCertDN(publicKey)
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Failed to extract DN from certificate: %v", err))
		return
	}
	logging.Easylog(logCh, "INFO", "Channel Certificates loaded successfully")

	//User-Agent 설정
	agentConfig := useragent.UserAgentConfig{
		AppName:    settings.AppName,
		AppVersion: settings.AppVersion,
		PartnerBIC: settings.PartnerBIC,
		CustomerIdentifier: useragent.CustomerIdentifier{
			Type:  "BIC",
			Value: settings.PartnerBIC,
		},
	}
	agent, err := useragent.BuildUserAgent(agentConfig, "Go")
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error building User-Agent: %v", err))
		return
	}
	fmt.Printf("User-Agent: %s\n\n", agent)
	logging.Easylog(logCh, "INFO", fmt.Sprintf("User-Agent: %s", agent))

	tokenData := &auth.TokenData{
		TokenPurpose:   "messaging",
		ConsumerKey:    settings.Messaging.ConsumerKey,
		ConsumerSecret: settings.Messaging.ConsumerSecret,
		Audience:       strings.TrimPrefix(settings.Messaging.TokenUrl, "https://"),
		Subject:        settings.Messaging.Subject,
		GrantType:      settings.Messaging.GrantType,
		Scope:          settings.Messaging.Scope,
		PrivateKey:     privateKey,
		PublicKey:      publicKey,
	}

	//Message Partner 구성
	for _, partner := range partners {
		partner.InputChannel = make(chan string, 1000)
		fsutil.EnsureDir(fsutil.PathHelper(partner.InputPath))
		fsutil.EnsureDir(fsutil.PathHelper(partner.OutputPath))
		fsutil.EnsureDir(fsutil.PathHelper(partner.AckPath))
	}

	//Worker 등록 및 shutdown 함수 생성
	startTokenService(&wg, settings, tokenData, logCh, exitCmd)
	shutdown := createShutdown(stopAll, &wg, &shutdownOnce, tokenData, settings, logCh, doneLogger)

	//main
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("Input: ")
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)
		switch input {
		case "exit":
			shutdown("Exit command received. Exiting application...")
			return
		case "1":
			tokenData.RLock()
			msgPurpose := tokenData.TokenPurpose
			msgAccessToken := tokenData.AccessToken
			tokenData.RUnlock()

			fmt.Println("----------------------------------------------")
			fmt.Printf("%s Token: %s\n", msgPurpose, msgAccessToken)
			fmt.Println("----------------------------------------------")
		case "2":
			//인증서 갱신
			auth.GetCert(settings, tokenData, logCh)
		case "3":
			//인증서 정보 출력
			auth.GetCertInfo(publicKey)
		}
	}
}

func startTokenService(wg *sync.WaitGroup, settings *config.Settings, tokenData *auth.TokenData, logCh chan logging.LogData, exitCmd chan bool) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		auth.TokenService(settings, tokenData, logCh, exitCmd)
	}()
}

func createShutdown(stopAll func(string), wg *sync.WaitGroup, shutdownOnce *sync.Once, tokenData *auth.TokenData, settings *config.Settings, logCh chan logging.LogData, doneLogger <-chan struct{}) func(string) {
	return func(reason string) {
		shutdownOnce.Do(func() {
			stopAll(reason)
			wg.Wait()
			auth.RevokeToken(settings, tokenData, logCh)
			close(logCh)
			<-doneLogger
		})
	}
}

func startLoggerService(logCh chan logging.LogData, exitCmd chan bool, doneLogger chan struct{}) {
	go func() {
		defer close(doneLogger)
		logging.Logger(exitCmd, logCh)
	}()
}
