package messaging

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Pocketwind/SWIFT-Launcher/auth"
	"github.com/Pocketwind/SWIFT-Launcher/config"
	"github.com/Pocketwind/SWIFT-Launcher/fsutil"
	"github.com/Pocketwind/SWIFT-Launcher/logging"
)

func DownloadService(settings *config.Settings, tokenData *auth.TokenData, partners []config.Partner, exitCmd <-chan bool, logCh chan<- logging.LogData) {
	logging.Easylog(logCh, "INFO", "Starting Download Service")
	ticker := time.NewTicker(time.Duration(settings.Messaging.UpdateInterval) * time.Second)
	defer ticker.Stop()
loop:
	for {
		select {
		case <-ticker.C:
			//distribution list 조회하기
			distributions, err := GetDistributions(settings, tokenData, logCh)
			if err != nil {
				logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error getting distributions: %v", err))
				continue
			}
			//Health Check
			logging.Easylog(logCh, "INFO", "Health Check - OK")
			//ID로 다운로드
			err = Download(settings, tokenData, distributions.Distributions, partners, logCh)
			if err != nil {
				logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error downloading distribution: %v", err))
				continue
			}
		case <-exitCmd:
			break loop
		}
	}

	logging.Easylog(logCh, "INFO", "Download Service stopped")
}

func Download(settings *config.Settings, tokenData *auth.TokenData, distributions []Distribution, partners []config.Partner, logCh chan<- logging.LogData) error {
	//다운로드 타입 정의
	var finMessages []string
	var finReports []string
	var interactMessages []string
	var interactReports []string
	var fileActMessages []string
	var fileActReports []string

	for _, dist := range distributions {
		switch dist.Service {
		case "fin":
			switch dist.Type {
			case "message":
				finMessages = append(finMessages, strconv.Itoa(dist.ID))
			case "transmissionReport":
				finReports = append(finReports, strconv.Itoa(dist.ID))
			}
		case "interAct":
			switch dist.Type {
			case "message":
				interactMessages = append(interactMessages, strconv.Itoa(dist.ID))
			case "transmissionReport":
				interactReports = append(interactReports, strconv.Itoa(dist.ID))
			}
		case "fileAct":
			switch dist.Type {
			case "message":
				fileActMessages = append(fileActMessages, strconv.Itoa(dist.ID))
			case "transmissionReport":
				fileActReports = append(fileActReports, strconv.Itoa(dist.ID))
			}
		}
	}

	var finPartners []config.Partner
	var interactPartners []config.Partner
	var fileActPartners []config.Partner
	//파트너별로 다운로드
	for _, partner := range partners {
		switch partner.Type {
		case "fin":
			finPartners = append(finPartners, partner)
		case "interAct":
			interactPartners = append(interactPartners, partner)
		case "fileAct":
			fileActPartners = append(fileActPartners, partner)
		}
	}
	go downloadFINMessages(settings, tokenData, finMessages, finPartners, logCh)
	go downloadFINReports(settings, tokenData, finReports, finPartners, logCh)
	go downloadInterActMessages(settings, tokenData, interactMessages, interactPartners, logCh)
	go downloadInterActReports(settings, tokenData, interactReports, interactPartners, logCh)
	//go downloadFileActMessages(settings, tokenData, fileActMessages, fileActPartners, logCh)
	//go downloadFileActReports(settings, tokenData, fileActReports, fileActPartners, logCh)

	return nil
}

func downloadInterActMessages(settings *config.Settings, tokenData *auth.TokenData, ids []string, partners []config.Partner, logCh chan<- logging.LogData) {
	if len(ids) == 0 {
		return
	}
	//Auth
	tokenData.RLock()
	tokenType := tokenData.TokenType
	accessToken := tokenData.AccessToken
	tokenData.RUnlock()

	//URL
	downloadUrl := settings.Messaging.InterActMessageUrl
	//ids
	idsJoined := strings.Join(ids, ",")
	ranges := ids[0] + "-" + ids[len(ids)-1]
	//request 만들기
	req, err := http.NewRequest("GET", downloadUrl, nil)
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error creating request: %v", err))
		return
	}
	//param
	query := req.URL.Query()
	query.Set("distribution-id", idsJoined)
	req.URL.RawQuery = query.Encode()
	//header
	req.Header.Set("Authorization", fmt.Sprintf("%s %s", tokenType, accessToken))
	req.Header.Set("Accept", "application/json")
	//proxy 사용해서 call
	client := settings.Messaging.HttpClient
	resp, err := client.Do(req)
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error making request: %v", err))
		return
	}
	defer resp.Body.Close()
	response, _ := io.ReadAll(resp.Body)
	/*
		//파일로 저장
		filePath := fsutil.PathHelper(settings.Messaging.DownloadPath) + "/" + ranges + ".json"
		err = os.WriteFile(filePath, response, 0644)
		if err != nil {
			logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error writing file for distribution %s: %v", ranges, err))
			return
		}
	*/
	//payload 분리
	var downloads []MXDownload
	err = json.Unmarshal(response, &downloads)
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error unmarshalling response for distribution %s: %v", ranges, err))
		return
	}
	//전문 생성 및 라우팅
	for _, message := range downloads {
		//파트너별로 라우팅
		for _, partner := range partners {
			if partner.Route == message.Message.Requestor || true { //라우팅 기능 임시 off 무조건 true
				outputPath := fsutil.PathHelper(partner.OutputPath)
				if tag := message.Distribution.DistributionTag; tag != "" {
					outputPath = fsutil.PathHelper(outputPath + "/" + tag)
				}
				fsutil.EnsureDir(outputPath)
				outputPath = fsutil.PathHelper(outputPath + "/" + strconv.Itoa(message.Distribution.ID) + partner.Extension)
				messageFile, err := MXMessageMaker(message)
				if err != nil {
					logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error creating MX message for distribution %d: %v", message.Distribution.ID, err))
					continue
				}
				err = os.WriteFile(outputPath, []byte(messageFile), 0644)
				if err != nil {
					logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error writing file for distribution %d: %v", message.Distribution.ID, err))
				}
				logging.Easylog(logCh, "INFO", fmt.Sprintf("Downloaded MX message for distribution %d to %s", message.Distribution.ID, outputPath))
			}
		}
	}
	//ACK 처리
	MultiAck(settings, tokenData, ids, logCh)
}

func downloadInterActReports(settings *config.Settings, tokenData *auth.TokenData, ids []string, partners []config.Partner, logCh chan<- logging.LogData) {
	if len(ids) == 0 {
		return
	}
	//Auth
	tokenData.RLock()
	tokenType := tokenData.TokenType
	accessToken := tokenData.AccessToken
	tokenData.RUnlock()
	//URL
	downloadUrl := settings.Messaging.InterActReportUrl
	//ids
	idsJoined := strings.Join(ids, ",")
	ranges := ids[0] + "-" + ids[len(ids)-1]
	//request 만들기
	req, err := http.NewRequest("GET", downloadUrl, nil)
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error creating request: %v", err))
		return
	}
	//param
	query := req.URL.Query()
	query.Set("distribution-id", idsJoined)
	req.URL.RawQuery = query.Encode()
	//header
	req.Header.Set("Authorization", fmt.Sprintf("%s %s", tokenType, accessToken))
	req.Header.Set("Accept", "application/json")
	//proxy 사용해서 call
	client := settings.Messaging.HttpClient
	resp, err := client.Do(req)
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error making request: %v", err))
		return
	}
	defer resp.Body.Close()
	response, _ := io.ReadAll(resp.Body)
	/*
		//파일로 저장
		filePath := fsutil.PathHelper(settings.Messaging.DownloadPath) + "/" + ranges + ".json"
		err = os.WriteFile(filePath, response, 0644)
		if err != nil {
			logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error writing file for distribution %s: %v", ranges, err))
			return
		}
	*/
	//payload 분리
	var reports []MXReport
	err = json.Unmarshal(response, &reports)
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error unmarshalling response for distribution %s: %v", ranges, err))
		return
	}
	//전문 생성 및 라우팅
	for _, report := range reports {
		//파트너별로 라우팅
		for _, partner := range partners {
			if partner.Route == report.TransmissionReport.Message.Requestor || true { //라우팅 기능 임시 off 무조건 true
				ackPath := fsutil.PathHelper(partner.AckPath)
				if tag := report.Distribution.DistributionTag; tag != "" {
					ackPath = fsutil.PathHelper(ackPath + "/" + tag)
				}
				fsutil.EnsureDir(ackPath)
				ackPath = fsutil.PathHelper(ackPath + "/" + strconv.Itoa(report.Distribution.ID) + partner.Extension)
				reportFile, err := MXReportMaker(report)
				if err != nil {
					logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error creating MX report for distribution %d: %v", report.Distribution.ID, err))
					continue
				}
				err = os.WriteFile(ackPath, []byte(reportFile), 0644)
				if err != nil {
					logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error writing file for distribution %d: %v", report.Distribution.ID, err))
				}
				logging.Easylog(logCh, "INFO", fmt.Sprintf("Downloaded MX report for distribution %d to %s", report.Distribution.ID, ackPath))
			}
		}
	}
	//ACK 처리
	MultiAck(settings, tokenData, ids, logCh)
}

func downloadFINReports(settings *config.Settings, tokenData *auth.TokenData, ids []string, partners []config.Partner, logCh chan<- logging.LogData) {
	if len(ids) == 0 {
		return
	}
	//Auth
	tokenData.RLock()
	tokenType := tokenData.TokenType
	accessToken := tokenData.AccessToken
	tokenData.RUnlock()
	//URL
	downloadUrl := settings.Messaging.FinReportUrl
	//ids
	idsJoined := strings.Join(ids, ",")
	ranges := ids[0] + "-" + ids[len(ids)-1]
	//request 만들기
	req, err := http.NewRequest("GET", downloadUrl, nil)
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error creating request: %v", err))
		return
	}
	//param
	query := req.URL.Query()
	query.Set("distribution-id", idsJoined)
	req.URL.RawQuery = query.Encode()
	//header
	req.Header.Set("Authorization", fmt.Sprintf("%s %s", tokenType, accessToken))
	req.Header.Set("Accept", "application/json")
	//proxy 사용해서 call
	client := settings.Messaging.HttpClient
	resp, err := client.Do(req)
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error making request: %v", err))
		return
	}
	defer resp.Body.Close()
	response, _ := io.ReadAll(resp.Body)
	/*
		//파일로 저장
		filePath := fsutil.PathHelper(settings.Messaging.DownloadPath) + "/" + ranges + ".json"
		err = os.WriteFile(filePath, response, 0644)
		if err != nil {
			logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error writing file for distribution %s: %v", ranges, err))
			return
		}
	*/

	//payload 분리
	var reports []MTReport
	err = json.Unmarshal(response, &reports)
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error unmarshalling response for distribution %s: %v", ranges, err))
		return
	}

	//전문 생성 및 라우팅
	for _, report := range reports {
		//파트너별로 라우팅
		for _, partner := range partners {
			if partner.Route == report.TransmissionReport.Message.Sender || true { //라우팅 기능 임시 off 무조건 true
				ackPath := fsutil.PathHelper(partner.AckPath)
				if tag := report.Distribution.DistributionTag; tag != "" {
					ackPath = fsutil.PathHelper(ackPath + "/" + tag)
				}
				fsutil.EnsureDir(ackPath)
				ackPath = fsutil.PathHelper(ackPath + "/" + strconv.Itoa(report.Distribution.ID) + partner.Extension)
				reportFile, err := FINReportMaker(report)
				if err != nil {
					logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error creating FIN report for distribution %d: %v", report.Distribution.ID, err))
					continue
				}
				err = os.WriteFile(ackPath, []byte(reportFile), 0644)
				if err != nil {
					logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error writing file for distribution %d: %v", report.Distribution.ID, err))
				}
				logging.Easylog(logCh, "INFO", fmt.Sprintf("Downloaded FIN report for distribution %d to %s", report.Distribution.ID, ackPath))
			}
		}
	}

	//ACK 처리
	MultiAck(settings, tokenData, ids, logCh)
}

func downloadFINMessages(settings *config.Settings, tokenData *auth.TokenData, ids []string, partners []config.Partner, logCh chan<- logging.LogData) {
	if len(ids) == 0 {
		return
	}

	tokenData.RLock()
	tokenType := tokenData.TokenType
	accessToken := tokenData.AccessToken
	tokenData.RUnlock()

	downloadUrl := settings.Messaging.FinMessageUrl
	//ids
	idsJoined := strings.Join(ids, ",")
	ranges := ids[0] + "-" + ids[len(ids)-1]
	//request 만들기
	req, err := http.NewRequest("GET", downloadUrl, nil)
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error creating request: %v", err))
		return
	}
	//param
	query := req.URL.Query()
	query.Set("distribution-id", idsJoined)
	req.URL.RawQuery = query.Encode()
	//header
	req.Header.Set("Authorization", fmt.Sprintf("%s %s", tokenType, accessToken))
	req.Header.Set("Accept", "application/json")
	//proxy 사용해서 call
	client := settings.Messaging.HttpClient
	resp, err := client.Do(req)
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error making request: %v", err))
		return
	}
	defer resp.Body.Close()
	response, _ := io.ReadAll(resp.Body)
	/*
		//파일로 저장
		filePath := fsutil.PathHelper(settings.Messaging.DownloadPath) + "/" + ranges + ".json"
		err = os.WriteFile(filePath, response, 0644)
		if err != nil {
			logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error writing file for distribution %s: %v", ranges, err))
			return
		}
	*/

	//payload 분리
	var downloads []MTDownload
	err = json.Unmarshal(response, &downloads)
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error unmarshalling response for distribution %s: %v", ranges, err))
		return
	}

	//전문 생성 및 라우팅
	for _, message := range downloads {
		//파트너별로 라우팅
		for _, partner := range partners {
			if partner.Route == message.Message.Receiver || true { //라우팅 기능 임시 off 무조건 true
				outputPath := fsutil.PathHelper(partner.OutputPath)
				if tag := message.Distribution.DistributionTag; tag != "" {
					outputPath = fsutil.PathHelper(outputPath + "/" + tag)
				}
				fsutil.EnsureDir(outputPath)
				outputPath = fsutil.PathHelper(outputPath + "/" + strconv.Itoa(message.Distribution.ID) + partner.Extension)
				messageFile, err := FINMessageMaker(message)
				if err != nil {
					logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error creating FIN message for distribution %d: %v", message.Distribution.ID, err))
					continue
				}
				err = os.WriteFile(outputPath, []byte(messageFile), 0644)
				if err != nil {
					logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error writing file for distribution %d: %v", message.Distribution.ID, err))
				}
				logging.Easylog(logCh, "INFO", fmt.Sprintf("Downloaded FIN message for distribution %d to %s", message.Distribution.ID, outputPath))
			}
		}
	}

	//ACK 처리
	MultiAck(settings, tokenData, ids, logCh)
}

func GetDistributions(settings *config.Settings, tokenData *auth.TokenData, logCh chan<- logging.LogData) (*Distributions, error) {
	//Auth
	auth.Auth(settings, tokenData, logCh)
	tokenData.RLock()
	tokenType := tokenData.TokenType
	accessToken := tokenData.AccessToken
	tokenData.RUnlock()

	//URL
	distUrl := settings.Messaging.DistributionUrl

	//param
	req, err := http.NewRequest("GET", distUrl, nil)
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error creating request: %v", err))
		return nil, err
	}
	query := req.URL.Query()
	query.Set("limit", strconv.Itoa(settings.Messaging.MaxDistributionSize))
	query.Set("offset", "0")
	req.URL.RawQuery = query.Encode()

	//header
	req.Header.Set("Authorization", fmt.Sprintf("%s %s", tokenType, accessToken))
	req.Header.Set("Accept", "application/json")

	//proxy 사용해서 call
	client := settings.Messaging.HttpClient
	resp, err := client.Do(req)
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error making request: %v", err))
		return nil, err
	}
	defer resp.Body.Close()
	response, _ := io.ReadAll(resp.Body)
	var distributions Distributions
	err = json.Unmarshal(response, &distributions)
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error unmarshalling response: %v", err))
		return nil, err
	}
	//data.Easylog(logCh, "INFO", fmt.Sprintln(string(response)))
	return &distributions, nil
}
