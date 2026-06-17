package messaging

import (
	"fmt"
	"os"
	"strings"

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
