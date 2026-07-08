package messaging

import (
	"crypto/md5"
	"encoding/base64"
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

	//input - ACK
	//output - message
	var finInputPartners []config.Partner
	var finOutputPartners []config.Partner
	var interActInputPartners []config.Partner
	var interActOutputPartners []config.Partner
	var fileActInputPartners []config.Partner
	var fileActOutputPartners []config.Partner
	//타입별로 분리
	for _, partner := range partners {
		//파트너 status false면 끄기
		if !partner.Status {
			logging.Easylog(logCh, "INFO", "Partner is disabled: "+partner.Name)
			continue
		}
		switch partner.Type {
		case "fin":
			switch partner.Direction {
			case "in":
				finInputPartners = append(finInputPartners, partner)
			case "out":
				finOutputPartners = append(finOutputPartners, partner)
			}
		case "interAct":
			switch partner.Direction {
			case "in":
				interActInputPartners = append(interActInputPartners, partner)
			case "out":
				interActOutputPartners = append(interActOutputPartners, partner)
			}
		case "fileAct":
			switch partner.Direction {
			case "in":
				fileActInputPartners = append(fileActInputPartners, partner)
			case "out":
				fileActOutputPartners = append(fileActOutputPartners, partner)
			}
		}
	}

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
	results := make(chan downloadResult, 6)

	tasks := []task{
		{mtype: finMsgTask, ids: finMessages},
		{mtype: finReportTask, ids: finReports},
		{mtype: interActMsgTask, ids: interactMessages},
		{mtype: interActReportTask, ids: interactReports},
		{mtype: fileActMsgTask, ids: fileActMessages},
		{mtype: fileActReportTask, ids: fileActReports},
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
				ackIDs, err = downloadFINMessages(settings, tokenData, finMessages, finOutputPartners, logCh)
			case finReportTask:
				ackIDs, err = downloadFINReports(settings, tokenData, finReports, finInputPartners, logCh)
			case interActMsgTask:
				ackIDs, err = downloadInterActMessages(settings, tokenData, interactMessages, interActOutputPartners, logCh)
			case interActReportTask:
				ackIDs, err = downloadInterActReports(settings, tokenData, interactReports, interActInputPartners, logCh)
			case fileActMsgTask:
				ackIDs, err = downloadFileActMessages(settings, tokenData, fileActMessages, fileActOutputPartners, logCh)
			case fileActReportTask:
				ackIDs, err = downloadFileActReports(settings, tokenData, fileActReports, fileActInputPartners, logCh)
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
		err := MultiAck(settings, tokenData, ackIDs, logCh)
		if err != nil && firstErr == nil {
			firstErr = err
		}
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
		return nil, fmt.Errorf("error creating request: %w", err)
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
		return nil, fmt.Errorf("error making request: %w", err)
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
		return nil, fmt.Errorf("error unmarshalling response for distribution %s: %w", ranges, err)
	}
	ackedSet := make(map[string]struct{})
	//전문 생성 및 라우팅
	for _, message := range downloads {
		distID := strconv.Itoa(message.Distribution.ID)
		routed := false
		written := false
		//base64 디코드
		messageDecoded, err := base64.StdEncoding.DecodeString(message.Message.Payload)
		if err != nil {
			logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error decoding MX message payload for distribution %d: %v", message.Distribution.ID, err))
			continue
		}
		message.Message.Payload = string(messageDecoded)
		//parse
		mx, err := MXParser(message.Message.Payload)
		if err != nil {
			logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error parsing MX message for distribution %d: %v", message.Distribution.ID, err))
			continue
		}
		message.Message.MX = mx
		//파트너별로 라우팅
		for _, partner := range partners {
			if MXRouter(partner.Route, message.Message) {
				//db 저장
				if dbErr := WriteMXMessageToSQL(message, partner.Name); dbErr != nil {
					logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error writing MX message to SQL for distribution %d: %v", message.Distribution.ID, dbErr))
					continue
				}
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
					break
				}
				err = os.WriteFile(outputPath, []byte(messageFile), 0644)
				if err != nil {
					logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error writing file for distribution %d: %v", message.Distribution.ID, err))
					break
				}
				written = true
				logging.Easylog(logCh, "INFO", fmt.Sprintf("Downloaded MX message for distribution %d to %s (%s)", message.Distribution.ID, outputPath, partner.Name))
				break
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
		return nil, fmt.Errorf("error creating request: %w", err)
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
		return nil, fmt.Errorf("error making request: %w", err)
	}
	defer resp.Body.Close()
	response, _ := io.ReadAll(resp.Body)
	//payload 분리
	var reports []MXReport
	err = json.Unmarshal(response, &reports)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling response for distribution %s: %w", ranges, err)
	}
	ackedSet := make(map[string]struct{})
	//전문 생성 및 라우팅
	for _, report := range reports {
		distID := strconv.Itoa(report.Distribution.ID)
		routed := false
		written := false
		//base64 디코드
		payloadDecoded, err := base64.StdEncoding.DecodeString(report.TransmissionReport.Message.Payload)
		if err != nil {
			logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error decoding payload for distribution %d: %v", report.Distribution.ID, err))
			continue
		}
		report.TransmissionReport.Message.Payload = string(payloadDecoded)
		//parse
		mx, err := MXParser(report.TransmissionReport.Message.Payload)
		if err != nil {
			logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error parsing MX message for distribution %d: %v", report.Distribution.ID, err))
			continue
		}
		report.TransmissionReport.Message.MX = mx
		//파트너별로 라우팅
		for _, partner := range partners {
			if MXRouter(partner.Route, report.TransmissionReport.Message) {
				//db 저장
				if dbErr := WriteMXReportToSQL(report, partner.Name); dbErr != nil {
					logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error writing MX report to SQL for distribution %d: %v", report.Distribution.ID, dbErr))
					continue
				}
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
					break
				}
				err = os.WriteFile(ackPath, []byte(reportFile), 0644)
				if err != nil {
					logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error writing file for distribution %d: %v", report.Distribution.ID, err))
					break
				}
				written = true
				logging.Easylog(logCh, "INFO", fmt.Sprintf("Downloaded MX report for distribution %d to %s (%s)", report.Distribution.ID, ackPath, partner.Name))
				break
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
		return nil, fmt.Errorf("error creating request: %w", err)
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
		return nil, fmt.Errorf("error making request: %w", err)
	}
	defer resp.Body.Close()
	response, _ := io.ReadAll(resp.Body)
	/*
		//파일로 저장
		filePath := fsutil.PathHelper(settings.Messaging.DownloadPath) + "/" + ranges + ".json"
		err = os.WriteFile(filePath, response, 0644)
		if err != nil {
			return nil, fmt.Errorf("error writing file for distribution %s: %w", ranges, err)
		}
	*/

	//payload 분리
	var reports []MTReport
	err = json.Unmarshal(response, &reports)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling response for distribution %s: %w", ranges, err)
	}
	ackedSet := make(map[string]struct{})

	//전문 생성 및 라우팅
	for _, report := range reports {
		distID := strconv.Itoa(report.Distribution.ID)
		routed := false
		written := false
		//base64 디코드
		payloadDecoded, err := base64.StdEncoding.DecodeString(report.TransmissionReport.Message.Payload)
		if err != nil {
			logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error decoding payload for distribution %d: %v", report.Distribution.ID, err))
			continue
		}
		report.TransmissionReport.Message.Payload = string(payloadDecoded)
		//ACK는 Direction Ack로 변경
		//report.TransmissionReport.Message.Direction = "Ack"
		//전문 구조화
		mt, err := MTParser(report.TransmissionReport.Message.Payload)
		report.TransmissionReport.Message.MT = mt
		//파트너별로 라우팅
		for _, partner := range partners {
			if MTRouter(partner.Route, report.TransmissionReport.Message) {
				//db 저장
				if err != nil {
					if dbErr := WriteMTAckToSQL(report, err, partner.Name); dbErr != nil {
						logging.Easylog(logCh, "WARN", fmt.Sprintf("Failed to persist MT parse failure for distribution %d: %v", report.Distribution.ID, dbErr))
					}
					logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error parsing MT message for distribution %d: %v", report.Distribution.ID, err))
					continue
				}
				if dbErr := WriteMTAckToSQL(report, nil, partner.Name); dbErr != nil {
					logging.Easylog(logCh, "WARN", fmt.Sprintf("Failed to persist MT parse success for distribution %d: %v", report.Distribution.ID, dbErr))
				}
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
					break
				}
				err = os.WriteFile(ackPath, []byte(reportFile), 0644)
				if err != nil {
					logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error writing file for distribution %d: %v", report.Distribution.ID, err))
					break
				}
				written = true
				logging.Easylog(logCh, "INFO", fmt.Sprintf("Downloaded FIN report for distribution %d to %s (%s)", report.Distribution.ID, ackPath, partner.Name))
				break
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
		return nil, fmt.Errorf("error creating request: %w", err)
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
		return nil, fmt.Errorf("error making request: %w", err)
	}
	defer resp.Body.Close()
	response, _ := io.ReadAll(resp.Body)
	/*
		//파일로 저장
		filePath := fsutil.PathHelper(settings.Messaging.DownloadPath) + "/" + ranges + ".json"
		err = os.WriteFile(filePath, response, 0644)
		if err != nil {
			return nil, fmt.Errorf("error writing file for distribution %s: %w", ranges, err)
		}
	*/

	//payload 분리
	var downloads []MTDownload
	err = json.Unmarshal(response, &downloads)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling response for distribution %s: %w", ranges, err)
	}
	ackedSet := make(map[string]struct{})

	//전문 생성 및 라우팅
	for _, message := range downloads {
		distID := strconv.Itoa(message.Distribution.ID)
		routed := false
		written := false
		//Payload base64 디코드
		payloadDecoded, err := base64.StdEncoding.DecodeString(message.Message.Payload)
		if err != nil {
			logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error decoding payload for distribution %d: %v", message.Distribution.ID, err))
			continue
		}
		message.Message.Payload = string(payloadDecoded)
		//전문 구조화
		mt, err := MTParser(message.Message.Payload)
		message.Message.MT = mt
		//logging.Easylog(logCh, "INFO", fmt.Sprintf("%v", mt))
		//파트너별로 라우팅
		for _, partner := range partners {
			if MTRouter(partner.Route, message.Message) {
				//db 저장
				if err != nil {
					if dbErr := WriteMTMessageToSQL(message, err, partner.Name); dbErr != nil {
						logging.Easylog(logCh, "WARN", fmt.Sprintf("Failed to persist MT parse failure for distribution %d: %v", message.Distribution.ID, dbErr))
					}
					logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error parsing MT message for distribution %d: %v", message.Distribution.ID, err))
					continue
				}
				message.Message.MT = mt
				if dbErr := WriteMTMessageToSQL(message, nil, partner.Name); dbErr != nil {
					logging.Easylog(logCh, "WARN", fmt.Sprintf("Failed to persist MT message to SQLite for distribution %d: %v", message.Distribution.ID, dbErr))
				}
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
					break
				}
				err = os.WriteFile(outputPath, []byte(messageFile), 0644)
				if err != nil {
					logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error writing file for distribution %d: %v", message.Distribution.ID, err))
					break
				}
				written = true
				logging.Easylog(logCh, "INFO", fmt.Sprintf("Downloaded FIN message for distribution %d to %s (%s)", message.Distribution.ID, outputPath, partner.Name))
				break
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

func downloadFileActReports(settings *config.Settings, tokenData *auth.TokenData, ids []string, partners []config.Partner, logCh chan<- logging.LogData) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	//Auth
	tokenData.RLock()
	tokenType := tokenData.TokenType
	accessToken := tokenData.AccessToken
	tokenData.RUnlock()

	//URL
	downloadUrl := settings.Messaging.FileActReportUrl

	//ids
	idsJoined := strings.Join(ids, ",")
	ranges := ids[0] + "-" + ids[len(ids)-1]

	//request 만들기
	req, err := http.NewRequest("GET", downloadUrl, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
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
		return nil, fmt.Errorf("error making request: %w", err)
	}
	defer resp.Body.Close()
	response, _ := io.ReadAll(resp.Body)

	//payload 분리
	var reports []FileActReport
	err = json.Unmarshal(response, &reports)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling response for distribution %s: %w", ranges, err)
	}
	ackedSet := make(map[string]struct{})

	//전문 생성 및 라우팅
	for _, report := range reports {
		distID := strconv.Itoa(report.Distribution.ID)
		routed := false
		written := false
		//파트너별로 라우팅
		for _, partner := range partners {
			if FileActRouter(partner, report.TransmissionReport.CompanionInfo) {
				routed = true
				ackPath := fsutil.PathHelper(partner.AckPath)
				if tag := report.Distribution.DistributionTag; tag != "" {
					ackPath = fsutil.PathHelper(ackPath + "/" + tag)
				}
				fsutil.EnsureDir(ackPath)
				ackPath = fsutil.PathHelper(ackPath + "/" + strconv.Itoa(report.Distribution.ID) + partner.Extension)
				reportFile, err := FileActReportMaker(report)
				if err != nil {
					logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error creating FileAct report for distribution %d: %v", report.Distribution.ID, err))
					break
				}
				err = os.WriteFile(ackPath, []byte(reportFile), 0644)
				if err != nil {
					logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error writing file for distribution %d: %v", report.Distribution.ID, err))
					break
				}
				written = true
				logging.Easylog(logCh, "INFO", fmt.Sprintf("Downloaded FileAct report for distribution %d to %s (%s)", report.Distribution.ID, ackPath, partner.Name))
				break
			}
		}
		if !routed {
			logging.Easylog(logCh, "WARN", fmt.Sprintf("No route matched for FileAct report distribution %s. Skipping ACK.", distID))
		} else if !written {
			logging.Easylog(logCh, "WARN", fmt.Sprintf("FileAct report distribution %s matched route but file write failed. Skipping ACK.", distID))
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
		return nil, fmt.Errorf("error creating request: %w", err)
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
		return nil, fmt.Errorf("error making request: %w", err)
	}
	defer resp.Body.Close()
	response, _ := io.ReadAll(resp.Body)
	var distributions Distributions
	err = json.Unmarshal(response, &distributions)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling response: %w", err)
	}
	//data.Easylog(logCh, "INFO", fmt.Sprintln(string(response)))
	return &distributions, nil
}

func downloadFileActMessages(settings *config.Settings, tokenData *auth.TokenData, ids []string, partners []config.Partner, logCh chan<- logging.LogData) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	//auth
	tokenData.RLock()
	tokenType := tokenData.TokenType
	accessToken := tokenData.AccessToken
	tokenData.RUnlock()

	//URL
	downloadUrl := settings.Messaging.FileActMessageUrl

	//request body
	//encryption key 임의 설정 32글자
	encKey := "01234567890123456789012345678901"
	encKeyB64 := base64.StdEncoding.EncodeToString([]byte(encKey))
	encKeyMD5 := md5.Sum([]byte(encKey))
	fileTransferRequest := FileTransferRequest{
		FileAttributes: FileAttributes{
			FileName: "temp.bin",
		},
		FileOperation: FileOperation{
			Type: "download",
		},
		EncryptionAttributes: EncryptionAttributes{
			KeyAlg:       "AES256",
			KeyValue:     encKeyB64,
			KeyDigestAlg: "MD5",
			KeyDigest:    base64.StdEncoding.EncodeToString(encKeyMD5[:]),
		},
	}

	//json body
	bodyBytes, err := json.Marshal(fileTransferRequest)
	if err != nil {
		return nil, fmt.Errorf("error marshalling request body: %w", err)
	}
	ackedSet := make(map[string]struct{})

	//FileAct는 한번에 하나만 다운가능
	//Initiate
	for _, id := range ids {
		routed := false
		written := false
		//request 만들기
		req, err := http.NewRequest("POST", downloadUrl, strings.NewReader(string(bodyBytes)))
		if err != nil {
			return nil, fmt.Errorf("error creating request: %w", err)
		}

		//param
		query := req.URL.Query()
		query.Set("distribution-id", id)
		req.URL.RawQuery = query.Encode()

		//header
		req.Header.Set("Authorization", fmt.Sprintf("%s %s", tokenType, accessToken))
		req.Header.Set("Accept", "application/json")

		//proxy 사용해서 call
		client := settings.Messaging.HttpClient
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("error making request: %w", err)
		}
		response, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("error reading response body: %w", err)
		}
		var fileActResponse Distribution
		err = json.Unmarshal(response, &fileActResponse)
		if err != nil {
			return nil, fmt.Errorf("error unmarshalling response: %w", err)
		}
		if len(fileActResponse.FileTransferResponse.SignedURLs) == 0 {
			logging.Easylog(logCh, "WARN", fmt.Sprintf("No signed URL for FileAct message distribution %s. Skipping ACK.", id))
			continue
		}
		//data.Easylog(logCh, "INFO", fmt.Sprintln(string(response)))

		//--------------------------------------------------------------------------------------

		//Download
		signedURL := fileActResponse.FileTransferResponse.SignedURLs[0].URL

		//파트너 라우팅
		for _, partner := range partners {
			if FileActRouter(partner, fileActResponse.CompanionInfo) {
				routed = true
				outputPath := fsutil.PathHelper(partner.OutputPath)
				if tag := fileActResponse.DistributionTag; tag != "" {
					outputPath = fsutil.PathHelper(outputPath + "/" + tag)
				}
				err = fsutil.EnsureDir(outputPath)
				if err != nil {
					logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error ensuring output dir for distribution %s: %v", id, err))
					break
				}
				outputPath = fsutil.PathHelper(outputPath + "/" + fileActResponse.CompanionInfo.SenderReference + partner.Extension)

				//Download file
				req, err := http.NewRequest("GET", signedURL, nil)
				if err != nil {
					logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error creating download request for distribution %s: %v", id, err))
					break
				}
				req.Header.Set("x-amz-server-side-encryption-customer-algorithm", "AES256")
				req.Header.Set("x-amz-server-side-encryption-customer-key", encKeyB64)
				req.Header.Set("x-amz-server-side-encryption-customer-key-MD5", base64.StdEncoding.EncodeToString(encKeyMD5[:]))

				client := settings.Messaging.HttpClient
				resp, err := client.Do(req)
				if err != nil {
					logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error making download request for distribution %s: %v", id, err))
					break
				}
				if resp.StatusCode < 200 || resp.StatusCode >= 300 {
					resp.Body.Close()
					logging.Easylog(logCh, "ERROR", fmt.Sprintf("Download request failed for distribution %s with status %s", id, resp.Status))
					break
				}

				//Write to file
				outFile, err := os.Create(outputPath)
				if err != nil {
					resp.Body.Close()
					logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error creating output file for distribution %s: %v", id, err))
					break
				}

				_, err = io.Copy(outFile, resp.Body)
				resp.Body.Close()
				if err != nil {
					outFile.Close()
					logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error writing output file for distribution %s: %v", id, err))
					break
				}
				err = outFile.Close()
				if err != nil {
					logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error closing output file for distribution %s: %v", id, err))
					break
				}

				written = true
				logging.Easylog(logCh, "INFO", fmt.Sprintf("Downloaded FileAct message for distribution %s to %s", fileActResponse.CompanionInfo.SenderReference, outputPath))
				break
			}
		}

		if !routed {
			logging.Easylog(logCh, "WARN", fmt.Sprintf("No route matched for FileAct message distribution %s. Skipping ACK.", id))
		} else if !written {
			logging.Easylog(logCh, "WARN", fmt.Sprintf("FileAct message distribution %s matched route but file write failed. Skipping ACK.", id))
		}
		if written {
			ackedSet[id] = struct{}{}
		}
	}

	ackedIDs := make([]string, 0, len(ackedSet))
	for id := range ackedSet {
		ackedIDs = append(ackedIDs, id)
	}

	return ackedIDs, nil
}
