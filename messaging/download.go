package messaging

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Pocketwind/SWIFT-Launcher/auth"
	"github.com/Pocketwind/SWIFT-Launcher/config"
	"github.com/Pocketwind/SWIFT-Launcher/fsutil"
	"github.com/Pocketwind/SWIFT-Launcher/logging"
)

func readDownloadResponse(settings *config.Settings, req *http.Request) ([]byte, error) {
	if settings.Messaging.HttpClient == nil {
		return nil, errors.New("messaging HTTP client is not configured")
	}
	resp, err := settings.Messaging.HttpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("download request returned HTTP %d", resp.StatusCode)
	}
	response, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read download response: %w", err)
	}
	return response, nil
}

func recordDownloadError(batchErr *error, logCh chan<- logging.LogData, err error) {
	*batchErr = errors.Join(*batchErr, err)
	logging.Easylog(logCh, "ERROR", err.Error())
}

func acknowledgedIDs(ids map[string]struct{}) []string {
	result := make([]string, 0, len(ids))
	for id := range ids {
		result = append(result, id)
	}
	sort.Strings(result)
	return result
}

func requestedDistribution(ids []string, id string) bool {
	for _, requested := range ids {
		if id == requested {
			return true
		}
	}
	return false
}

func distributionOutputPath(base, tag, name string) (string, error) {
	if strings.TrimSpace(base) == "" {
		return "", errors.New("output directory is empty")
	}
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `\/:`) {
		return "", fmt.Errorf("invalid output file name %q", name)
	}
	tag = strings.ReplaceAll(tag, `\`, "/")
	if filepath.IsAbs(tag) || strings.HasPrefix(tag, "/") || strings.Contains(tag, ":") {
		return "", fmt.Errorf("invalid distribution tag %q", tag)
	}
	for _, part := range strings.Split(tag, "/") {
		if part == ".." {
			return "", fmt.Errorf("distribution tag escapes output directory: %q", tag)
		}
	}
	path := filepath.Join(base, filepath.FromSlash(tag), name)
	if !fsutil.IsPathUnderDir(path, base) {
		return "", errors.New("download path escapes output directory")
	}
	if err := fsutil.EnsureDir(filepath.Dir(path)); err != nil {
		return "", err
	}
	return path, nil
}

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
	if len(distributions) == 0 {
		return nil
	}
	if err := auth.Auth(settings, tokenData, logCh); err != nil {
		return fmt.Errorf("authenticate download: %w", err)
	}
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
		firstErr = errors.Join(firstErr, r.err)
		for _, id := range r.ids {
			ackSet[id] = struct{}{}
		}
	}

	ackIDs := make([]string, 0, len(ackSet))
	for id := range ackSet {
		ackIDs = append(ackIDs, id)
	}
	sort.Strings(ackIDs)
	if len(ackIDs) > 0 {
		// Files are visible before ACK. If ACK fails, redelivery can recreate a
		// file already consumed downstream; consumers must deduplicate by ID.
		err := MultiAck(settings, tokenData, ackIDs, logCh)
		firstErr = errors.Join(firstErr, err)
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
	response, err := readDownloadResponse(settings, req)
	if err != nil {
		return nil, err
	}
	/*
		//파일로 저장
		filePath := fsutil.PathHelper(settings.Messaging.DownloadPath) + "/" + ranges + ".json"
		err = fsutil.AtomicWriteFile(filePath, response, 0644)
		if err != nil {
			recordDownloadError(&downloadErr, logCh, fmt.Errorf("Error writing file for distribution %s: %v", ranges, err))
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
	var downloadErr error
	//전문 생성 및 라우팅
	for _, message := range downloads {
		distID := strconv.Itoa(message.Distribution.ID)
		if !requestedDistribution(ids, distID) {
			recordDownloadError(&downloadErr, logCh, fmt.Errorf("unexpected distribution ID %s in download response", distID))
			continue
		}
		routed := false
		written := false
		//base64 디코드
		messageDecoded, err := base64.StdEncoding.DecodeString(message.Message.Payload)
		if err != nil {
			recordDownloadError(&downloadErr, logCh, fmt.Errorf("Error decoding MX message payload for distribution %d: %v", message.Distribution.ID, err))
			continue
		}
		message.Message.Payload = string(messageDecoded)
		//parse
		mx, err := MXParser(message.Message.Payload)
		if err != nil {
			recordDownloadError(&downloadErr, logCh, fmt.Errorf("Error parsing MX message for distribution %d: %v", message.Distribution.ID, err))
			continue
		}
		message.Message.MX = mx
		//파트너별로 라우팅
		for _, partner := range partners {
			if MXRouter(partner.Route, message.Message) {
				routed = true
				//db 저장
				if dbErr := WriteMXMessageToSQL(message, partner.Name); dbErr != nil {
					logging.Easylog(logCh, "WARN", fmt.Sprintf("Optional MX message archive failed for distribution %d; continuing file delivery: %v", message.Distribution.ID, dbErr))
				}
				outputPath, err := distributionOutputPath(partner.OutputPath, message.Distribution.DistributionTag, strconv.Itoa(message.Distribution.ID)+partner.Extension)
				if err != nil {
					recordDownloadError(&downloadErr, logCh, fmt.Errorf("output path for distribution %s: %w", distID, err))
					break
				}
				messageFile, err := MXMessageMaker(message)
				if err != nil {
					recordDownloadError(&downloadErr, logCh, fmt.Errorf("Error creating MX message for distribution %d: %v", message.Distribution.ID, err))
					break
				}
				err = fsutil.AtomicWriteFile(outputPath, []byte(messageFile), 0644)
				if err != nil {
					recordDownloadError(&downloadErr, logCh, fmt.Errorf("Error writing file for distribution %d: %v", message.Distribution.ID, err))
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
	return ackedIDs, downloadErr
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
	response, err := readDownloadResponse(settings, req)
	if err != nil {
		return nil, err
	}
	//payload 분리
	var reports []MXReport
	err = json.Unmarshal(response, &reports)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling response for distribution %s: %w", ranges, err)
	}
	ackedSet := make(map[string]struct{})
	var downloadErr error
	//전문 생성 및 라우팅
	for _, report := range reports {
		distID := strconv.Itoa(report.Distribution.ID)
		if !requestedDistribution(ids, distID) {
			recordDownloadError(&downloadErr, logCh, fmt.Errorf("unexpected distribution ID %s in download response", distID))
			continue
		}
		routed := false
		written := false
		//base64 디코드
		payloadDecoded, err := base64.StdEncoding.DecodeString(report.TransmissionReport.Message.Payload)
		if err != nil {
			recordDownloadError(&downloadErr, logCh, fmt.Errorf("Error decoding payload for distribution %d: %v", report.Distribution.ID, err))
			continue
		}
		report.TransmissionReport.Message.Payload = string(payloadDecoded)
		//parse
		mx, err := MXParser(report.TransmissionReport.Message.Payload)
		if err != nil {
			recordDownloadError(&downloadErr, logCh, fmt.Errorf("Error parsing MX message for distribution %d: %v", report.Distribution.ID, err))
			continue
		}
		report.TransmissionReport.Message.MX = mx
		//파트너별로 라우팅
		for _, partner := range partners {
			if MXRouter(partner.Route, report.TransmissionReport.Message) {
				routed = true
				//db 저장
				if dbErr := WriteMXReportToSQL(report, partner.Name); dbErr != nil {
					logging.Easylog(logCh, "WARN", fmt.Sprintf("Optional MX report archive failed for distribution %d; continuing file delivery: %v", report.Distribution.ID, dbErr))
				}
				ackPath, err := distributionOutputPath(partner.AckPath, report.Distribution.DistributionTag, strconv.Itoa(report.Distribution.ID)+partner.Extension)
				if err != nil {
					recordDownloadError(&downloadErr, logCh, fmt.Errorf("output path for distribution %s: %w", distID, err))
					break
				}
				reportFile, err := MXReportMaker(report)
				if err != nil {
					recordDownloadError(&downloadErr, logCh, fmt.Errorf("Error creating MX report for distribution %d: %v", report.Distribution.ID, err))
					break
				}
				err = fsutil.AtomicWriteFile(ackPath, []byte(reportFile), 0644)
				if err != nil {
					recordDownloadError(&downloadErr, logCh, fmt.Errorf("Error writing file for distribution %d: %v", report.Distribution.ID, err))
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
	return ackedIDs, downloadErr
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
	response, err := readDownloadResponse(settings, req)
	if err != nil {
		return nil, err
	}
	/*
		//파일로 저장
		filePath := fsutil.PathHelper(settings.Messaging.DownloadPath) + "/" + ranges + ".json"
		err = fsutil.AtomicWriteFile(filePath, response, 0644)
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
	var downloadErr error

	//전문 생성 및 라우팅
	for _, report := range reports {
		distID := strconv.Itoa(report.Distribution.ID)
		if !requestedDistribution(ids, distID) {
			recordDownloadError(&downloadErr, logCh, fmt.Errorf("unexpected distribution ID %s in download response", distID))
			continue
		}
		routed := false
		written := false
		//base64 디코드
		payloadDecoded, err := base64.StdEncoding.DecodeString(report.TransmissionReport.Message.Payload)
		if err != nil {
			recordDownloadError(&downloadErr, logCh, fmt.Errorf("Error decoding payload for distribution %d: %v", report.Distribution.ID, err))
			continue
		}
		report.TransmissionReport.Message.Payload = string(payloadDecoded)
		//ACK는 Direction Ack로 변경
		//report.TransmissionReport.Message.Direction = "Ack"
		//전문 구조화
		mt, err := MTParser(report.TransmissionReport.Message.Payload)
		if err != nil {
			recordDownloadError(&downloadErr, logCh, fmt.Errorf("parse FIN payload for distribution %s: %w", distID, err))
			continue
		}
		report.TransmissionReport.Message.MT = mt
		//파트너별로 라우팅
		for _, partner := range partners {
			if MTRouter(partner.Route, report.TransmissionReport.Message) {
				routed = true
				//db 저장
				dbErr := WriteMTReportToSQL(report, partner.Name)
				if dbErr != nil {
					logging.Easylog(logCh, "WARN", fmt.Sprintf("Optional MT report archive failed for distribution %d; continuing file delivery: %v", report.Distribution.ID, dbErr))
				}
				ackPath, err := distributionOutputPath(partner.AckPath, report.Distribution.DistributionTag, strconv.Itoa(report.Distribution.ID)+partner.Extension)
				if err != nil {
					recordDownloadError(&downloadErr, logCh, fmt.Errorf("output path for distribution %s: %w", distID, err))
					break
				}
				reportFile, err := FINReportMaker(report)
				if err != nil {
					recordDownloadError(&downloadErr, logCh, fmt.Errorf("Error creating FIN report for distribution %d: %v", report.Distribution.ID, err))
					break
				}
				err = fsutil.AtomicWriteFile(ackPath, []byte(reportFile), 0644)
				if err != nil {
					recordDownloadError(&downloadErr, logCh, fmt.Errorf("Error writing file for distribution %d: %v", report.Distribution.ID, err))
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
	return ackedIDs, downloadErr
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
	response, err := readDownloadResponse(settings, req)
	if err != nil {
		return nil, err
	}
	/*
		//파일로 저장
		filePath := fsutil.PathHelper(settings.Messaging.DownloadPath) + "/" + ranges + ".json"
		err = fsutil.AtomicWriteFile(filePath, response, 0644)
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
	var downloadErr error

	//전문 생성 및 라우팅
	for _, message := range downloads {
		distID := strconv.Itoa(message.Distribution.ID)
		if !requestedDistribution(ids, distID) {
			recordDownloadError(&downloadErr, logCh, fmt.Errorf("unexpected distribution ID %s in download response", distID))
			continue
		}
		routed := false
		written := false
		//Payload base64 디코드
		payloadDecoded, err := base64.StdEncoding.DecodeString(message.Message.Payload)
		if err != nil {
			recordDownloadError(&downloadErr, logCh, fmt.Errorf("Error decoding payload for distribution %d: %v", message.Distribution.ID, err))
			continue
		}
		message.Message.Payload = string(payloadDecoded)
		//전문 구조화
		mt, err := MTParser(message.Message.Payload)
		if err != nil {
			recordDownloadError(&downloadErr, logCh, fmt.Errorf("parse FIN payload for distribution %s: %w", distID, err))
			continue
		}
		message.Message.MT = mt
		//logging.Easylog(logCh, "INFO", fmt.Sprintf("%v", mt))
		//파트너별로 라우팅
		for _, partner := range partners {
			if MTRouter(partner.Route, message.Message) {
				routed = true
				//db 저장
				dbErr := WriteMTMessageToSQL(message, partner.Name)
				if dbErr != nil {
					logging.Easylog(logCh, "WARN", fmt.Sprintf("Optional MT message archive failed for distribution %d; continuing file delivery: %v", message.Distribution.ID, dbErr))
				}
				outputPath, err := distributionOutputPath(partner.OutputPath, message.Distribution.DistributionTag, strconv.Itoa(message.Distribution.ID)+partner.Extension)
				if err != nil {
					recordDownloadError(&downloadErr, logCh, fmt.Errorf("output path for distribution %s: %w", distID, err))
					break
				}
				messageFile, err := FINMessageMaker(message)
				if err != nil {
					recordDownloadError(&downloadErr, logCh, fmt.Errorf("Error creating FIN message for distribution %d: %v", message.Distribution.ID, err))
					break
				}
				err = fsutil.AtomicWriteFile(outputPath, []byte(messageFile), 0644)
				if err != nil {
					recordDownloadError(&downloadErr, logCh, fmt.Errorf("Error writing file for distribution %d: %v", message.Distribution.ID, err))
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
	return ackedIDs, downloadErr
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
	response, err := readDownloadResponse(settings, req)
	if err != nil {
		return nil, err
	}

	//payload 분리
	var reports []FileActReport
	err = json.Unmarshal(response, &reports)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling response for distribution %s: %w", ranges, err)
	}
	ackedSet := make(map[string]struct{})
	var downloadErr error

	//전문 생성 및 라우팅
	for _, report := range reports {
		distID := strconv.Itoa(report.Distribution.ID)
		if !requestedDistribution(ids, distID) {
			recordDownloadError(&downloadErr, logCh, fmt.Errorf("unexpected distribution ID %s in download response", distID))
			continue
		}
		routed := false
		written := false
		//파트너별로 라우팅
		for _, partner := range partners {
			if FileActRouter(partner, report.TransmissionReport.CompanionInfo) {
				routed = true
				ackPath, err := distributionOutputPath(partner.AckPath, report.Distribution.DistributionTag, strconv.Itoa(report.Distribution.ID)+partner.Extension)
				if err != nil {
					recordDownloadError(&downloadErr, logCh, fmt.Errorf("output path for distribution %s: %w", distID, err))
					break
				}
				reportFile, err := FileActReportMaker(report)
				if err != nil {
					recordDownloadError(&downloadErr, logCh, fmt.Errorf("Error creating FileAct report for distribution %d: %v", report.Distribution.ID, err))
					break
				}
				err = fsutil.AtomicWriteFile(ackPath, []byte(reportFile), 0644)
				if err != nil {
					recordDownloadError(&downloadErr, logCh, fmt.Errorf("Error writing file for distribution %d: %v", report.Distribution.ID, err))
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
	return ackedIDs, downloadErr
}

func GetDistributions(settings *config.Settings, tokenData *auth.TokenData, logCh chan<- logging.LogData) (*Distributions, error) {
	//Auth
	if err := auth.Auth(settings, tokenData, logCh); err != nil {
		return nil, fmt.Errorf("authenticate distributions: %w", err)
	}
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
	response, err := readDownloadResponse(settings, req)
	if err != nil {
		return nil, err
	}
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
	encKey := make([]byte, 32)
	if _, err := rand.Read(encKey); err != nil {
		return nil, fmt.Errorf("generate download encryption key: %w", err)
	}
	encKeyB64 := base64.StdEncoding.EncodeToString(encKey)
	encKeyMD5 := md5.Sum(encKey)
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
	var downloadErr error

	//FileAct는 한번에 하나만 다운가능
	//Initiate
	for _, id := range ids {
		routed := false
		written := false
		//request 만들기
		req, err := http.NewRequest("POST", downloadUrl, strings.NewReader(string(bodyBytes)))
		if err != nil {
			return acknowledgedIDs(ackedSet), errors.Join(downloadErr, fmt.Errorf("error creating request: %w", err))
		}

		//param
		query := req.URL.Query()
		query.Set("distribution-id", id)
		req.URL.RawQuery = query.Encode()

		//header
		req.Header.Set("Authorization", fmt.Sprintf("%s %s", tokenType, accessToken))
		req.Header.Set("Accept", "application/json")

		//proxy 사용해서 call
		req.Header.Set("Content-Type", "application/json")
		response, err := readDownloadResponse(settings, req)
		if err != nil {
			return acknowledgedIDs(ackedSet), errors.Join(downloadErr, fmt.Errorf("initiate FileAct distribution %s: %w", id, err))
		}
		var fileActResponse Distribution
		err = json.Unmarshal(response, &fileActResponse)
		if err != nil {
			return acknowledgedIDs(ackedSet), errors.Join(downloadErr, fmt.Errorf("error unmarshalling response: %w", err))
		}
		if len(fileActResponse.FileTransferResponse.SignedURLs) != 1 {
			recordDownloadError(&downloadErr, logCh, fmt.Errorf("expected one signed URL for FileAct distribution %s, got %d; skipping ACK", id, len(fileActResponse.FileTransferResponse.SignedURLs)))
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
				outputPath, err := distributionOutputPath(partner.OutputPath, fileActResponse.DistributionTag, fileActResponse.CompanionInfo.SenderReference+partner.Extension)
				if err != nil {
					recordDownloadError(&downloadErr, logCh, fmt.Errorf("output path for distribution %s: %w", id, err))
					break
				}

				//Download file
				req, err := http.NewRequest("GET", signedURL, nil)
				if err != nil {
					recordDownloadError(&downloadErr, logCh, fmt.Errorf("Error creating download request for distribution %s: %v", id, err))
					break
				}
				req.Header.Set("x-amz-server-side-encryption-customer-algorithm", "AES256")
				req.Header.Set("x-amz-server-side-encryption-customer-key", encKeyB64)
				req.Header.Set("x-amz-server-side-encryption-customer-key-MD5", base64.StdEncoding.EncodeToString(encKeyMD5[:]))

				client := settings.Messaging.HttpClient
				resp, err := client.Do(req)
				if err != nil {
					recordDownloadError(&downloadErr, logCh, fmt.Errorf("Error making download request for distribution %s: %v", id, err))
					break
				}
				if resp.StatusCode < 200 || resp.StatusCode >= 300 {
					resp.Body.Close()
					recordDownloadError(&downloadErr, logCh, fmt.Errorf("Download request failed for distribution %s with status %s", id, resp.Status))
					break
				}

				//Write to file
				err = fsutil.AtomicWriteReader(outputPath, resp.Body, 0644)
				resp.Body.Close()
				if err != nil {
					recordDownloadError(&downloadErr, logCh, fmt.Errorf("write FileAct distribution %s: %w", id, err))
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

	return ackedIDs, downloadErr
}
