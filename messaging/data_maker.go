package messaging

import (
	"crypto/md5"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Pocketwind/SWIFT-Launcher/config"
	"github.com/Pocketwind/SWIFT-Launcher/fsutil"
	"github.com/Pocketwind/SWIFT-Launcher/logging"
	"github.com/antchfx/xmlquery"
)

func MTDataMaker(path string, logCh chan<- logging.LogData) (MTData, error) {
	//파일 열기
	file, err := os.ReadFile(path)
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error opening file: %v", err))
		return MTData{}, fmt.Errorf("error opening file: %w", err)
	}

	//string 변환
	str := string(file)

	findata := MTData{}

	block1 := strings.Index(str, "{1:")
	block2 := strings.Index(str, "{2:")
	trnStart := strings.Index(str, ":20:")
	if block1 < 0 || block2 < 0 || trnStart < 0 {
		return MTData{}, fmt.Errorf("invalid FIN message format")
	}

	payload, err := extractBlock4(str)
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error extracting block 4: %v", err))
		return MTData{}, err
	}

	trnTail := str[trnStart+4:]
	trnEnd := strings.Index(trnTail, "\r\n")
	if trnEnd < 0 {
		trnEnd = strings.Index(trnTail, "\n")
	}
	if trnEnd < 0 {
		trnEnd = len(trnTail)
	}

	if block1+18 > len(str) || block2+19 > len(str) || block2+7 > len(str) {
		return MTData{}, fmt.Errorf("invalid FIN header ranges")
	}

	findata.Sender = str[block1+6 : block1+18]
	findata.Receiver = str[block2+7 : block2+19]
	findata.MessageType = "fin." + str[block2+4:block2+7]
	findata.Payload = payload
	findata.SenderReference = strings.TrimSpace(trnTail[:trnEnd])

	return findata, nil
}

func MXDataMaker(path string, logCh chan<- logging.LogData) (MXData, error) {
	//파일 열기
	file, err := os.ReadFile(path)
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error opening file: %v", err))
		return MXData{}, fmt.Errorf("error opening file: %w", err)
	}

	str := string(file)
	doc, err := xmlquery.Parse(strings.NewReader(str))
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error parsing XML: %v", err))
		return MXData{}, fmt.Errorf("error parsing XML: %w", err)
	}

	mxdata := MXData{}
	mxdata.MessageType = fsutil.SafeInnerText(xmlquery.FindOne(doc, "//*[local-name()='MessageIdentifier']"))
	mxdata.Requestor = fsutil.SafeInnerText(xmlquery.FindOne(doc, "//*[local-name()='Sender']/*[local-name()='DN']"))
	mxdata.Responder = fsutil.SafeInnerText(xmlquery.FindOne(doc, "//*[local-name()='Receiver']/*[local-name()='DN']"))
	mxdata.SenderReference = fsutil.SafeInnerText(xmlquery.FindOne(doc, "//*[local-name()='SenderReference']"))
	mxdata.ServiceCode = fsutil.SafeInnerText(xmlquery.FindOne(doc, "//*[local-name()='NetworkInfo']/*[local-name()='Service']"))
	mxdata.Format = "MX"
	mxdata.Payload = xmlquery.FindOne(doc, "//*[local-name()='Body']").OutputXML(false)
	mxdata.Payload = "<envelope:Envelope xmlns:envelope=\"urn:swift:xsd:envelope\">" + mxdata.Payload + "</envelope:Envelope>"

	//fmt.Printf("Parsed MXData: %+v\n", mxdata)

	return mxdata, nil
}

func extractBlock4(message string) (string, error) {
	start := strings.Index(message, "{4:")
	if start == -1 {
		return "", fmt.Errorf("block 4 start not found")
	}

	start += len("{4:")
	if start < len(message) {
		if strings.HasPrefix(message[start:], "\r\n") {
			start += 2
		} else if strings.HasPrefix(message[start:], "\n") {
			start += 1
		}
	}

	end := strings.Index(message[start:], "-}")
	if end == -1 {
		return "", fmt.Errorf("block 4 end not found")
	}

	payload := message[start : start+end]
	return strings.TrimRight(payload, "\r\n"), nil
}

func FileActDataMaker(path string, partner *config.Partner, logCh chan<- logging.LogData) (FAData, error) {
	//xml 파일 열기
	file, err := os.ReadFile(path)
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error opening file: %v", err))
		return FAData{}, fmt.Errorf("error opening file: %w", err)
	}

	//xml 파싱
	doc, err := xmlquery.Parse(strings.NewReader(string(file)))
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error parsing XML: %v", err))
		return FAData{}, fmt.Errorf("error parsing XML: %w", err)
	}

	//값 추출
	senderReference := fsutil.SafeInnerText(xmlquery.FindOne(doc, "//*[local-name()='SenderReference']"))
	messageIdentifier := fsutil.SafeInnerText(xmlquery.FindOne(doc, "//*[local-name()='MessageIdentifier']"))
	sender := fsutil.SafeInnerText(xmlquery.FindOne(doc, "//*[local-name()='Sender']/*[local-name()='DN']"))
	receiver := fsutil.SafeInnerText(xmlquery.FindOne(doc, "//*[local-name()='Receiver']/*[local-name()='DN']"))
	service := fsutil.SafeInnerText(xmlquery.FindOne(doc, "//*[local-name()='NetworkInfo']/*[local-name()='Service']"))
	fileLogicalName := fsutil.SafeInnerText(xmlquery.FindOne(doc, "//*[local-name()='FileLogicalName']"))
	body := fsutil.SafeInnerText(xmlquery.FindOne(doc, "//*[local-name()='Body']"))
	fileInfo := fsutil.SafeInnerText(xmlquery.FindOne(doc, "//*[local-name()='FileInfo']"))

	//FAData 구조체 생성
	var fadata FAData

	//fadata 채우기
	fadata.CompanionInfo.SenderReference = senderReference
	fadata.CompanionInfo.MessageType = messageIdentifier
	fadata.CompanionInfo.ServiceCode = service
	fadata.CompanionInfo.Requestor = sender
	fadata.CompanionInfo.Responder = receiver
	fadata.CompanionInfo.FileLogicalName = fileLogicalName
	fadata.CompanionInfo.NetworkInfo.FileInfo = fileInfo
	fadata.FileTransferRequest.FileAttributes.FileName = fileLogicalName
	fadata.FileTransferRequest.FileOperation.Type = "upload"
	fadata.FileTransferRequest.EncryptionAttributes.KeyAlg = "AES256"
	fadata.FileTransferRequest.EncryptionAttributes.KeyValue = "01234567890123456789012345678901" //임의의 32글자 키
	fadata.FileTransferRequest.EncryptionAttributes.KeyDigestAlg = "MD5"
	fadata.FileTransferRequest.EncryptionAttributes.KeyDigest = "sQqNsWTgdUEFt6mb5y4/5Q=="

	//bodypath
	body = fsutil.PathHelper(partner.InputPath + "/" + fsutil.GetFileName(body))

	//파일 쓰여질때까지 대기
	err = fsutil.WaitFileReady(body, 30*time.Second)
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error waiting for file ready: %v", err))
		return FAData{}, fmt.Errorf("error waiting for file ready: %w", err)
	}

	//업로드 대상 파일 로드
	payloadFile, err := os.ReadFile(body)
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error opening payload file: %v", err))
		return FAData{}, fmt.Errorf("error opening payload file: %w", err)
	}

	//filesize
	filesize := len(payloadFile)

	//MD5
	md5Hash := md5.Sum(payloadFile)

	//MD5 Base64
	md5HashB64 := base64.StdEncoding.EncodeToString(md5Hash[:])

	//encryption key 임의 설정 32글자
	encKey := "01234567890123456789012345678901"
	encKeyB64 := base64.StdEncoding.EncodeToString([]byte(encKey))
	encKeyMD5 := md5.Sum([]byte(encKey))

	//나머지 fadata채우기
	fadata.FileTransferRequest.FileAttributes.FileSize = filesize
	fadata.FileTransferRequest.FileAttributes.FileDigestAlg = "MD5"
	fadata.FileTransferRequest.FileAttributes.FileDigest = md5HashB64
	fadata.FileTransferRequest.EncryptionAttributes.KeyValue = encKeyB64
	fadata.FileTransferRequest.EncryptionAttributes.KeyDigest = base64.StdEncoding.EncodeToString(encKeyMD5[:])

	return fadata, nil
}

func DFADataMaker(filePath string, partner *config.Partner, logCh chan<- logging.LogData) (FAData, error) {
	var fadata FAData

	//파일 쓰여질때까지 대기
	err := fsutil.WaitFileReady(filePath, 5*time.Second)
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error waiting for file ready: %v", err))
		return FAData{}, fmt.Errorf("error waiting for file ready: %w", err)
	}

	//업로드 대상 파일 로드
	payloadFile, err := os.ReadFile(filePath)
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error opening payload file: %v", err))
		return FAData{}, fmt.Errorf("error opening payload file: %w", err)
	}

	//filename
	filename := filepath.Base(filePath)

	//filesize
	filesize := len(payloadFile)

	//MD5
	md5Hash := md5.Sum(payloadFile)

	//MD5 Base64
	md5HashB64 := base64.StdEncoding.EncodeToString(md5Hash[:])

	//encryption key 임의 설정 32글자
	encKey := "01234567890123456789012345678901"
	encKeyB64 := base64.StdEncoding.EncodeToString([]byte(encKey))
	encKeyMD5 := md5.Sum([]byte(encKey))

	fadata.FileTransferRequest.FileAttributes.FileName = filename
	fadata.FileTransferRequest.FileAttributes.FileSize = filesize
	fadata.FileTransferRequest.FileAttributes.FileDigest = md5HashB64
	fadata.FileTransferRequest.FileAttributes.FileDigestAlg = "MD5"
	fadata.FileTransferRequest.FileOperation.Type = "upload"
	fadata.FileTransferRequest.EncryptionAttributes.KeyAlg = "AES256"
	fadata.FileTransferRequest.EncryptionAttributes.KeyValue = encKeyB64
	fadata.FileTransferRequest.EncryptionAttributes.KeyDigestAlg = "MD5"
	fadata.FileTransferRequest.EncryptionAttributes.KeyDigest = base64.StdEncoding.EncodeToString(encKeyMD5[:])
	fadata.CompanionInfo.SenderReference = filename
	fadata.CompanionInfo.MessageType = partner.DFAInfo.RequestType
	fadata.CompanionInfo.ServiceCode = partner.DFAInfo.ServiceCode
	fadata.CompanionInfo.Requestor = partner.DFAInfo.Requestor
	fadata.CompanionInfo.Responder = partner.DFAInfo.Responder
	fadata.CompanionInfo.FileLogicalName = filepath.Base(filename)
	//fadata.CompanionInfo.Body = filePath
	fadata.CompanionInfo.NetworkInfo.RequestType = partner.DFAInfo.RequestType
	fadata.CompanionInfo.NetworkInfo.TransferDescription = partner.DFAInfo.TransferDescription
	fadata.CompanionInfo.NetworkInfo.TransferInfo = partner.DFAInfo.TransferInfo
	fadata.CompanionInfo.NetworkInfo.FileDescription = partner.DFAInfo.FileDescription
	fadata.CompanionInfo.NetworkInfo.FileInfo = partner.DFAInfo.FileInfo
	fadata.CompanionInfo.NetworkInfo.HeaderInfo = partner.DFAInfo.HeaderInfo

	return fadata, nil
}

func FileActReportMaker(report FileActReport) (string, error) {
	//base64 디코딩
	reportBytes, err := base64.StdEncoding.DecodeString(report.TransmissionReport.Report)
	if err != nil {
		return "", fmt.Errorf("error decoding report: %w", err)
	}
	return string(reportBytes), nil
}
