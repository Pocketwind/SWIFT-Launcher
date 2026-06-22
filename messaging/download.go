package messaging

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Pocketwind/SWIFT-Launcher/auth"
	"github.com/Pocketwind/SWIFT-Launcher/config"
	"github.com/Pocketwind/SWIFT-Launcher/fsutil"
	"github.com/Pocketwind/SWIFT-Launcher/logging"
)

func DownloadService(settings *config.Settings, tokenData *auth.TokenData, partners []config.Partner, exitCmd <-chan bool, logCh chan<- logging.LogData) {
	logging.Easylog(logCh, "INFO", "Starting Download Service")
	if settings.Messaging.UpdateInterval <= 0 {
		logging.Easylog(logCh, "INFO", "Update interval is set to 0 or negative. Download service will not run automatically.")
		<-exitCmd
		logging.Easylog(logCh, "INFO", "Download Service Stopped")
		return
	}
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

	//고루틴 중복호출 문제
	/*
		go downloadFINMessages(settings, tokenData, finMessages, finPartners, logCh)
		go downloadFINReports(settings, tokenData, finReports, finPartners, logCh)
		go downloadInterActMessages(settings, tokenData, interactMessages, interactPartners, logCh)
		go downloadInterActReports(settings, tokenData, interactReports, interactPartners, logCh)
		//go downloadFileActMessages(settings, tokenData, fileActMessages, fileActPartners, logCh)
		//go downloadFileActReports(settings, tokenData, fileActReports, fileActPartners, logCh)
	*/

	var wg sync.WaitGroup
	results := make(chan downloadResult, 4)

	tasks := []task{
		{mtype: finMsgTask, ids: finMessages},
		{mtype: finReportTask, ids: finReports},
		{mtype: interActMsgTask, ids: interactMessages},
		{mtype: interActReportTask, ids: interactReports},
	}

	for _, t := range tasks {
		if len(t.ids) == 0 {
			continue
		}

		wg.Add(1)
		go func(t task) {
			defer wg.Done()

			var err error
			var ackIDs []string
			switch t.mtype {
			case finMsgTask:
				ackIDs, err = downloadFINMessages(settings, tokenData, finMessages, finPartners, logCh)
			case finReportTask:
				ackIDs, err = downloadFINReports(settings, tokenData, finReports, finPartners, logCh)
			case interActMsgTask:
				ackIDs, err = downloadInterActMessages(settings, tokenData, interactMessages, interactPartners, logCh)
			case interActReportTask:
				ackIDs, err = downloadInterActReports(settings, tokenData, interactReports, interactPartners, logCh)
			}

			results <- downloadResult{ids: ackIDs, err: err}
		}(t)
	}
	go func() {
		wg.Wait()
		close(results)
	}()
	ackSet := make(map[string]struct{})
	var firstErr error

	for r := range results {
		if r.err != nil && firstErr == nil {
			firstErr = r.err
			continue
		}
		for _, id := range r.ids {
			ackSet[id] = struct{}{}
		}
	}

	ackIDs := make([]string, 0, len(ackSet))
	for id := range ackSet {
		ackIDs = append(ackIDs, id)
	}
	if len(ackIDs) > 0 {
		MultiAck(settings, tokenData, ackIDs, logCh)
	}

	return firstErr
}

func downloadInterActMessages(settings *config.Settings, tokenData *auth.TokenData, ids []string, partners []config.Partner, logCh chan<- logging.LogData) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
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
		return nil, err
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
		return nil, err
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
		return nil, err
	}
	ackedSet := make(map[string]struct{})
	//전문 생성 및 라우팅
	for _, message := range downloads {
		distID := strconv.Itoa(message.Distribution.ID)
		routed := false
		written := false
		//파트너별로 라우팅
		for _, partner := range partners {
			if MXRouter(partner.Route, message.Message) { //라우팅 기능 임시 off 무조건 true
				routed = true
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
					continue
				}
				written = true
				logging.Easylog(logCh, "INFO", fmt.Sprintf("Downloaded MX message for distribution %d to %s", message.Distribution.ID, outputPath))
			}
		}
		if !routed {
			logging.Easylog(logCh, "WARN", fmt.Sprintf("No route matched for MX message distribution %s. Skipping ACK.", distID))
		} else if !written {
			logging.Easylog(logCh, "WARN", fmt.Sprintf("MX message distribution %s matched route but file write failed. Skipping ACK.", distID))
		}
		if written {
			ackedSet[distID] = struct{}{}
		}
	}
	ackedIDs := make([]string, 0, len(ackedSet))
	for id := range ackedSet {
		ackedIDs = append(ackedIDs, id)
	}
	//ACK 처리 변경
	//MultiAck(settings, tokenData, ids, logCh)
	return ackedIDs, nil
}

func downloadInterActReports(settings *config.Settings, tokenData *auth.TokenData, ids []string, partners []config.Partner, logCh chan<- logging.LogData) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
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
		return nil, err
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
		return nil, err
	}
	defer resp.Body.Close()
	response, _ := io.ReadAll(resp.Body)
	/*
		//파일로 저장
		filePath := fsutil.PathHelper(settings.Messaging.DownloadPath) + "/" + ranges + ".json"
		err = os.WriteFile(filePath, response, 0644)
		if err != nil {
			logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error writing file for distribution %s: %v", ranges, err))
			return err
		}
	*/
	//payload 분리
	var reports []MXReport
	err = json.Unmarshal(response, &reports)
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error unmarshalling response for distribution %s: %v", ranges, err))
		return nil, err
	}
	ackedSet := make(map[string]struct{})
	//전문 생성 및 라우팅
	for _, report := range reports {
		distID := strconv.Itoa(report.Distribution.ID)
		routed := false
		written := false
		//파트너별로 라우팅
		for _, partner := range partners {
			if MXRouter(partner.Route, report.TransmissionReport.Message) { //라우팅 기능 임시 off 무조건 true
				routed = true
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
					continue
				}
				written = true
				logging.Easylog(logCh, "INFO", fmt.Sprintf("Downloaded MX report for distribution %d to %s", report.Distribution.ID, ackPath))
			}
		}
		if !routed {
			logging.Easylog(logCh, "WARN", fmt.Sprintf("No route matched for MX report distribution %s. Skipping ACK.", distID))
		} else if !written {
			logging.Easylog(logCh, "WARN", fmt.Sprintf("MX report distribution %s matched route but file write failed. Skipping ACK.", distID))
		}
		if written {
			ackedSet[distID] = struct{}{}
		}
	}
	ackedIDs := make([]string, 0, len(ackedSet))
	for id := range ackedSet {
		ackedIDs = append(ackedIDs, id)
	}
	//ACK 처리
	//MultiAck(settings, tokenData, ids, logCh)
	return ackedIDs, nil
}

func downloadFINReports(settings *config.Settings, tokenData *auth.TokenData, ids []string, partners []config.Partner, logCh chan<- logging.LogData) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
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
		return nil, err
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
		return nil, err
	}
	defer resp.Body.Close()
	response, _ := io.ReadAll(resp.Body)
	/*
		//파일로 저장
		filePath := fsutil.PathHelper(settings.Messaging.DownloadPath) + "/" + ranges + ".json"
		err = os.WriteFile(filePath, response, 0644)
		if err != nil {
			logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error writing file for distribution %s: %v", ranges, err))
			return err
		}
	*/

	//payload 분리
	var reports []MTReport
	err = json.Unmarshal(response, &reports)
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error unmarshalling response for distribution %s: %v", ranges, err))
		return nil, err
	}
	ackedSet := make(map[string]struct{})

	//전문 생성 및 라우팅
	for _, report := range reports {
		distID := strconv.Itoa(report.Distribution.ID)
		routed := false
		written := false
		//파트너별로 라우팅
		for _, partner := range partners {
			if MTRouter(partner.Route, report.TransmissionReport.Message) { //라우팅 기능 임시 off 무조건 true
				routed = true
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
					continue
				}
				written = true
				logging.Easylog(logCh, "INFO", fmt.Sprintf("Downloaded FIN report for distribution %d to %s", report.Distribution.ID, ackPath))
			}
		}
		if !routed {
			logging.Easylog(logCh, "WARN", fmt.Sprintf("No route matched for FIN report distribution %s. Skipping ACK.", distID))
		} else if !written {
			logging.Easylog(logCh, "WARN", fmt.Sprintf("FIN report distribution %s matched route but file write failed. Skipping ACK.", distID))
		}
		if written {
			ackedSet[distID] = struct{}{}
		}
	}
	ackedIDs := make([]string, 0, len(ackedSet))
	for id := range ackedSet {
		ackedIDs = append(ackedIDs, id)
	}

	//ACK 처리
	//MultiAck(settings, tokenData, ids, logCh)
	return ackedIDs, nil
}

func downloadFINMessages(settings *config.Settings, tokenData *auth.TokenData, ids []string, partners []config.Partner, logCh chan<- logging.LogData) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
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
		return nil, err
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
		return nil, err
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
		return nil, err
	}
	ackedSet := make(map[string]struct{})

	//전문 생성 및 라우팅
	for _, message := range downloads {
		distID := strconv.Itoa(message.Distribution.ID)
		routed := false
		written := false
		//파트너별로 라우팅
		for _, partner := range partners {
			if MTRouter(partner.Route, message.Message) { //라우팅 기능 임시 off 무조건 true
				routed = true
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
					continue
				}
				written = true
				logging.Easylog(logCh, "INFO", fmt.Sprintf("Downloaded FIN message for distribution %d to %s", message.Distribution.ID, outputPath))
			}
		}
		if !routed {
			logging.Easylog(logCh, "WARN", fmt.Sprintf("No route matched for FIN message distribution %s. Skipping ACK.", distID))
		} else if !written {
			logging.Easylog(logCh, "WARN", fmt.Sprintf("FIN message distribution %s matched route but file write failed. Skipping ACK.", distID))
		}
		if written {
			ackedSet[distID] = struct{}{}
		}
	}
	ackedIDs := make([]string, 0, len(ackedSet))
	for id := range ackedSet {
		ackedIDs = append(ackedIDs, id)
	}

	//ACK 처리
	//MultiAck(settings, tokenData, ids, logCh)
	return ackedIDs, nil
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
