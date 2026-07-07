package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Pocketwind/SWIFT-Launcher/auth"
	"github.com/Pocketwind/SWIFT-Launcher/config"
	"github.com/Pocketwind/SWIFT-Launcher/fsutil"
	"github.com/Pocketwind/SWIFT-Launcher/httpclient"
	"github.com/Pocketwind/SWIFT-Launcher/logging"
	"github.com/Pocketwind/SWIFT-Launcher/messaging"
	"github.com/Pocketwind/SWIFT-Launcher/useragent"
	"github.com/kardianos/service"
)

func main() {
	svcConfig := &service.Config{
		Name:        "SWIFT-Launcher-Service",
		DisplayName: "SWIFT-Launcher-Service",
		Description: "SWIFT messaging service",
	}

	prg := &program{}
	s, err := service.New(prg, svcConfig)
	if err != nil {
		fmt.Printf("Failed to create service: %v\n", err)
		os.Exit(1)
	}

	if len(os.Args) > 1 {
		cmd := strings.ToLower(os.Args[1])
		switch cmd {
		case "install", "uninstall", "start", "stop", "restart":
			if err := service.Control(s, cmd); err != nil {
				fmt.Printf("Service %s failed: %v\n", cmd, err)
				os.Exit(1)
			}
			fmt.Printf("Service %s succeeded\n", cmd)
			return
		case "status":
			type runtimeStatus struct {
				UptimeSeconds int64               `json:"uptime_seconds"`
				AccessToken   string              `json:"access_token"`
				Partners      []config.StatusData `json:"partners"`
			}

			svcStatus := "unknown"
			currentSt, err := s.Status()
			if err == nil {
				if currentSt == service.StatusRunning {
					svcStatus = "running"
				} else if currentSt == service.StatusStopped {
					svcStatus = "stopped"
				}
			}
			fmt.Printf("Service status: %s\n", svcStatus)

			// 런타임 상태 조회
			cfg, cfgErr := config.LoadSettings("settings.json")
			if cfgErr != nil || cfg.Status.Port <= 0 {
				return
			}
			hc := &http.Client{Timeout: 2 * time.Second}
			rtResp, rtErr := hc.Get(fmt.Sprintf("http://127.0.0.1:%d/status", cfg.Status.Port))
			if rtErr != nil {
				fmt.Println("Runtime status: unavailable")
				return
			}
			defer rtResp.Body.Close()
			var rt runtimeStatus
			if jsonErr := json.NewDecoder(rtResp.Body).Decode(&rt); jsonErr != nil {
				fmt.Println("Runtime status: parse error")
				return
			}
			fmt.Println("---------- Runtime Status ----------")
			fmt.Printf("Uptime       : %d seconds\n", rt.UptimeSeconds)
			if rt.AccessToken == "" {
				fmt.Println("Access Token : (not obtained)")
			} else {
				fmt.Printf("Access Token : %s\n", rt.AccessToken)
			}
			fmt.Printf("Partners     : %d\n", len(rt.Partners))
			for _, partner := range rt.Partners {
				fmt.Printf("  - %s (%s) : %t - %s\n", partner.Name, partner.Direction, partner.Status, partner.Route)
			}
			fmt.Println("------------------------------------")
			return
		case "console":
			app(true, nil)
			return
		case "help":
			showHelp()
			return
		default:
			showHelp()
			os.Exit(1)
		}
	}

	if service.Interactive() {
		app(true, nil)
		return
	}

	if err := s.Run(); err != nil {
		fmt.Printf("Service run failed: %v\n", err)
		os.Exit(1)
	}
}

func app(interactive bool, serviceStop <-chan struct{}) {
	startTime := time.Now()
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		if chdirErr := os.Chdir(exeDir); chdirErr != nil {
			fmt.Printf("WARN: Failed to set working directory to %s: %v\n", exeDir, chdirErr)
		}
	}
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
	shutdownEarly := func(reason string) {
		stopAll(reason)
		close(logCh)
		<-doneLogger
		if !interactive {
			os.Exit(1)
		}
	}

	//settings 로드
	settings, err := config.LoadSettings("settings.json")
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Failed to load settings: %v", err))
		fmt.Printf("ERROR: Failed to load settings: %v\n", err)
		shutdownEarly("Startup aborted: failed to load settings")
		return
	}
	logging.Easylog(logCh, "INFO", "Settings loaded successfully")

	//HTTP 클라이언트 생성
	settings.Messaging.HttpClient, err = httpclient.MakeHTTPClient(settings)
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Failed to create HTTP client: %v", err))
		fmt.Printf("ERROR: Failed to create HTTP client: %v\n", err)
		shutdownEarly("Startup aborted: failed to create HTTP client")
		return
	}

	//파트너 파일 로드
	partners, err := config.LoadPartners(settings.Messaging.PartnerFilePath)
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Failed to load partner file: %v", err))
		shutdownEarly("Startup aborted: failed to load partner file")
		return
	}
	logging.Easylog(logCh, "INFO", "Partner file loaded successfully")

	//채널 인증서 체크(초기실행 체크)
	_, errCert := os.Stat("pem/channel.cer")
	_, errKey := os.Stat("pem/channel.key")
	if os.IsNotExist(errCert) || os.IsNotExist(errKey) {
		if interactive {
			logging.Easylog(logCh, "INFO", "No channel certificate found. Starting certificate setup...")
			fmt.Println("No channel certificate found. Starting certificate setup...")
			err := auth.GetCert(settings, nil, logCh)
			if err != nil {
				logging.Easylog(logCh, "ERROR", fmt.Sprintf("Failed to get certificate: %v", err))
				fmt.Printf("ERROR: Failed to get certificate: %v\n", err)
				shutdownEarly("Startup aborted: certificate setup failed")
				return
			}
		} else {
			logging.Easylog(logCh, "ERROR", "No channel certificate found. Please run the application in interactive mode to set up the certificate.")
			fmt.Println("ERROR: No channel certificate found. Please run the application in interactive mode to set up the certificate.")
			//서비스모드 인증서 없으면 강제종료
			shutdownEarly("Startup aborted: channel certificate missing in service mode")
			return
		}
	}

	//키 파일 로드
	publicKeyBytes, err := os.ReadFile("pem/channel.cer")
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Failed to read public key file: %v", err))
		fmt.Printf("ERROR: Failed to read public key file: %v\n", err)
		shutdownEarly("Startup aborted: failed to read public key file")
		return
	}
	privateKeyBytes, err := os.ReadFile("pem/channel.key")
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Failed to read private key file: %v", err))
		fmt.Printf("ERROR: Failed to read private key file: %v\n", err)
		shutdownEarly("Startup aborted: failed to read private key file")
		return
	}
	publicKey := string(publicKeyBytes)
	privateKey := string(privateKeyBytes)

	//DN 추출
	settings.Messaging.Subject, err = auth.GetCertDN(publicKey)
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Failed to extract DN from certificate: %v", err))
		shutdownEarly("Startup aborted: failed to extract certificate DN")
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

	//IO 파트너 구분
	var inputPartners []config.Partner
	var outputPartners []config.Partner
	for i := range partners {
		switch partners[i].Direction {
		case "in":
			partners[i].InputChannel = make(chan string, 1000)
			inputPartners = append(inputPartners, partners[i])
		case "out":
			outputPartners = append(outputPartners, partners[i])
		default:
			logging.Easylog(logCh, "WARN", fmt.Sprintf("Unknown partner direction for %s: %s", partners[i].Name, partners[i].Direction))
		}
	}

	//Input Partner 구성
	for i := range inputPartners {
		err = fsutil.EnsureDir(fsutil.PathHelper(inputPartners[i].InputPath))
		if err != nil {
			logging.Easylog(logCh, "ERROR", fmt.Sprintf("Failed to ensure input directory: %v", err))
			fmt.Printf("ERROR: Failed to ensure input directory: %v\n", err)
			shutdownEarly("Startup aborted: failed to ensure input directory")
			return
		}
		err = fsutil.EnsureDir(fsutil.PathHelper(inputPartners[i].AckPath))
		if err != nil {
			logging.Easylog(logCh, "ERROR", fmt.Sprintf("Failed to ensure ack directory: %v", err))
			fmt.Printf("ERROR: Failed to ensure ack directory: %v\n", err)
			shutdownEarly("Startup aborted: failed to ensure ack directory")
			return
		}
		err = fsutil.EnsureDir(fsutil.PathHelper(inputPartners[i].ErrorPath))
		if err != nil {
			logging.Easylog(logCh, "ERROR", fmt.Sprintf("Failed to ensure error directory: %v", err))
			fmt.Printf("ERROR: Failed to ensure error directory: %v\n", err)
			shutdownEarly("Startup aborted: failed to ensure error directory")
			return
		}
		err = fsutil.EnsureDir(fsutil.PathHelper(inputPartners[i].ProgressPath))
		if err != nil {
			logging.Easylog(logCh, "ERROR", fmt.Sprintf("Failed to ensure progress directory: %v", err))
			fmt.Printf("ERROR: Failed to ensure progress directory: %v\n", err)
			shutdownEarly("Startup aborted: failed to ensure progress directory")
			return
		}
	}
	//Output Partner 구성
	for i := range outputPartners {
		err = fsutil.EnsureDir(fsutil.PathHelper(outputPartners[i].OutputPath))
		if err != nil {
			logging.Easylog(logCh, "ERROR", fmt.Sprintf("Failed to ensure output directory: %v", err))
			fmt.Printf("ERROR: Failed to ensure output directory: %v\n", err)
			return
		}
	}

	//토큰 먼저 발급받고 실행
	auth.Auth(settings, tokenData, logCh)
	//Worker 등록 및 shutdown 함수 생성
	startTokenService(&wg, settings, tokenData, logCh, exitCmd)
	shutdown := createShutdown(stopAll, &wg, &shutdownOnce, tokenData, settings, logCh, doneLogger)
	//Status 서비스 시작
	startStatusService(&wg, settings, tokenData, partners, logCh, exitCmd, startTime)
	for i := range inputPartners {
		//파트너 파일 watcher 시작
		wg.Add(1)
		go func(partner *config.Partner) {
			defer wg.Done()
			fsutil.WatchFileService(partner, exitCmd, logCh)
		}(&inputPartners[i])
	}
	//Collector 서비스 시작 (Input)
	for i := range inputPartners {
		//파트너 파일 watcher 시작
		wg.Add(1)
		go func(partner *config.Partner) {
			defer wg.Done()
			messaging.CollectorService(settings, partner, tokenData, logCh, exitCmd)
		}(&inputPartners[i])
	}
	//Download 서비스 시작(Output)
	wg.Add(1)
	go func() {
		defer wg.Done()
		messaging.DownloadService(settings, tokenData, partners, exitCmd, logCh)
	}()

	// Ctrl+C / 종료 시그널 처리
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	if !interactive {
		logging.Easylog(logCh, "INFO", "Service mode enabled")
		if serviceStop == nil {
			select {
			case <-sigCh:
				shutdown("Stop signal detected. Exiting application...")
			}
			return
		}
		select {
		case <-serviceStop:
			shutdown("Service stop requested. Exiting application...")
		case <-sigCh:
			shutdown("Stop signal detected. Exiting application...")
		}
		return
	}

	go func() {
		<-sigCh
		shutdown("Ctrl+C detected. Exiting application...")
		os.Exit(0)
	}()

	//main (interactive)
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("Input: ")
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)
		switch input {
		case "exit":
			shutdown("Exit command received. Exiting application...")
			return
		case "info":
			tokenData.RLock()
			msgPurpose := tokenData.TokenPurpose
			msgAccessToken := tokenData.AccessToken
			tokenData.RUnlock()

			fmt.Println("----------------------------------------------")
			fmt.Printf("%s Token: %s\n", msgPurpose, msgAccessToken)
			fmt.Println("----------------------------------------------")
		case "refresh":
			//인증서 갱신
			auth.GetCert(settings, tokenData, logCh)
		case "certinfo":
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

			waitDone := make(chan struct{})
			go func() {
				wg.Wait()
				close(waitDone)
			}()

			const shutdownWaitTimeout = 5 * time.Second
			select {
			case <-waitDone:
				auth.RevokeToken(settings, tokenData, logCh)
				close(logCh)
				<-doneLogger
			case <-time.After(shutdownWaitTimeout):
				// lingering worker가 있으면 log 채널 close 시 panic 가능성이 있어 강제 대기만 중단한다.
				logging.Easylog(logCh, "WARN", fmt.Sprintf("Shutdown wait timeout (%s). Some workers may still be stopping.", shutdownWaitTimeout))
			}
		})
	}
}

func startLoggerService(logCh chan logging.LogData, exitCmd chan bool, doneLogger chan struct{}) {
	go func() {
		defer close(doneLogger)
		logging.Logger(exitCmd, logCh)
	}()
}

type program struct {
	stopChan chan struct{}
	doneChan chan struct{}
	stopOnce sync.Once
}

func (p *program) Start(s service.Service) error {
	p.stopChan = make(chan struct{})
	p.doneChan = make(chan struct{})
	go func() {
		defer close(p.doneChan)
		app(false, p.stopChan)
	}()
	return nil
}

func (p *program) Stop(s service.Service) error {
	p.stopOnce.Do(func() {
		close(p.stopChan)
	})
	<-p.doneChan
	return nil
}

func startStatusService(wg *sync.WaitGroup, settings *config.Settings, tokenData *auth.TokenData, partners []config.Partner, logCh chan logging.LogData, exitCmd <-chan bool, startTime time.Time) {
	if settings.Status.Port <= 0 {
		logging.Easylog(logCh, "INFO", "Status service disabled (port not configured)")
		return
	}

	addr := fmt.Sprintf("127.0.0.1:%d", settings.Status.Port)

	mux := http.NewServeMux()
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		tokenData.RLock()
		accessToken := tokenData.AccessToken
		tokenData.RUnlock()

		partnerList := make([]config.StatusData, 0, len(partners))
		for _, p := range partners {
			partnerList = append(partnerList, config.StatusData{
				Name:         p.Name,
				Direction:    p.Direction,
				Status:       p.Status,
				Type:         p.Type,
				InputPath:    p.InputPath,
				OutputPath:   p.OutputPath,
				AckPath:      p.AckPath,
				ErrorPath:    p.ErrorPath,
				ProgressPath: p.ProgressPath,
				Extension:    p.Extension,
				IsDFA:        p.IsDFA,
				DFAInfo:      p.DFAInfo,
				Route:        p.Route,
			})
		}

		resp := map[string]any{
			"uptime_seconds": int64(time.Since(startTime).Seconds()),
			"access_token":   accessToken,
			"partners":       partnerList,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	server := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		IdleTimeout:  30 * time.Second,
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		logging.Easylog(logCh, "INFO", fmt.Sprintf("Status service listening on %s", addr))
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logging.Easylog(logCh, "ERROR", fmt.Sprintf("Status service error: %v", err))
		}
		logging.Easylog(logCh, "INFO", "Status service stopped")
	}()

	// exitCmd 감지 시 graceful shutdown
	go func() {
		<-exitCmd
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()
}

func showHelp() {
	fmt.Println("Available commands:")
	fmt.Println("install   - Install the service")
	fmt.Println("uninstall - Uninstall the service")
	fmt.Println("start     - Start the service")
	fmt.Println("stop      - Stop the service")
	fmt.Println("restart   - Restart the service")
	fmt.Println("status    - Show service status")
	fmt.Println("console   - Run in console mode")
	fmt.Println("help      - Show this help")
	fmt.Println("status    - Show partners and runtime status")
}
