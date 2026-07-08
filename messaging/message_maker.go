package messaging

import (
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"strings"
	"time"

	"github.com/Pocketwind/SWIFT-Launcher/fsutil"
	"github.com/antchfx/xmlquery"
)

func FINMessageMaker(messages MTDownload) (string, error) {
	var stringBuilder strings.Builder

	//Start of the block 1
	stringBuilder.WriteString("{1:F")
	//FIN Message Type (전문이니까 21)
	stringBuilder.WriteString("21")
	//Sender
	stringBuilder.WriteString(messages.Message.Sender)
	//Session Number
	stringBuilder.WriteString(fmt.Sprintf("%04d", messages.Message.NetworkInfo.SessionNumber))
	//Sequence Number
	stringBuilder.WriteString(fmt.Sprintf("%06d", messages.Message.NetworkInfo.SequenceNumber))
	//End of the block 1
	stringBuilder.WriteString("}")

	//Start of the block 2
	stringBuilder.WriteString("{2:")
	//Input or Output (Incoming이 SWIFT OUTPUT)
	if messages.Message.Direction == "Incoming" {
		stringBuilder.WriteString("O")
	} else {
		stringBuilder.WriteString("I")
	}
	//Message Type
	stringBuilder.WriteString(strings.Split(messages.Message.MessageType, ".")[1])
	//Receiver
	stringBuilder.WriteString(messages.Message.Receiver)
	//Priority (System, Urgent, Normal)
	if messages.Message.NetworkInfo.NetworkPriority == "System" {
		stringBuilder.WriteString("S")
	} else if messages.Message.NetworkInfo.NetworkPriority == "Urgent" {
		stringBuilder.WriteString("U")
	} else {
		stringBuilder.WriteString("N")
	}
	//End of the block 2
	stringBuilder.WriteString("}")

	//Start of the block 4
	stringBuilder.WriteString("{4:")
	//Payload
	stringBuilder.WriteString(messages.Message.Payload)
	//End of the block 4
	stringBuilder.WriteString("\r\n-}")

	return stringBuilder.String(), nil
}

func MXMessageMaker(message MXDownload) (string, error) {
	var MXHeaderData MXHeader

	//Header (Message태그)
	//Sender Reference
	MXHeaderData.SenderReference = message.Message.SenderReference
	//Message Identifier
	MXHeaderData.MessageIdentifier = message.Message.MessageType
	//Format
	MXHeaderData.Format = message.Message.Format
	//SubFormat (I/O)
	if message.Message.Direction == "Incoming" {
		MXHeaderData.SubFormat = "Output"
	} else {
		MXHeaderData.SubFormat = "Input"
	}
	//Sender DN
	MXHeaderData.SenderDN = message.Message.Requestor
	//Sender FullName X1, X2
	sender := strings.Split(message.Message.Requestor, ",")
	MXHeaderData.SenderFullNameX2 = strings.Split(sender[0], "=")[1]
	MXHeaderData.SenderFullNameX1 = strings.ToUpper(strings.Split(sender[1], "=")[1] + MXHeaderData.SenderFullNameX2)
	//Receiver DN
	MXHeaderData.ReceiverDN = message.Message.Responder
	//Receiver FullName X1, X2
	receiver := strings.Split(message.Message.Responder, ",")
	MXHeaderData.ReceiverFullNameX2 = strings.Split(receiver[0], "=")[1]
	MXHeaderData.ReceiverFullNameX1 = strings.ToUpper(strings.Split(receiver[1], "=")[1] + MXHeaderData.ReceiverFullNameX2)
	//InterfaceInfo Create, Context, Nature
	MXHeaderData.InterfaceInfoCreator = "Messenger"
	MXHeaderData.InterfaceInfoContext = "Original"
	MXHeaderData.InterfaceInfoNature = "Financial"
	//NetworkInfo Priority
	MXHeaderData.NetworkInfoPriority = message.Message.NetworkInfo.NetworkPriority
	//PD
	MXHeaderData.NetworkInfoDuplicate = message.Message.NetworkInfo.PossibleDuplicate
	//Service
	MXHeaderData.NetworkInfoService = message.Message.ServiceCode
	//Request Type
	MXHeaderData.NetworkInfoSWIFTNetType = message.Message.Format
	MXHeaderData.NetworkInfoSWIFTNetSubtype = message.Message.UsageIdentifier

	//Header XML 생성
	type HeaderWrapper struct {
		XMLName xml.Name `xml:"Header"`
		Inner   MXHeader `xml:"Message"`
	}
	headerBytes, err := xml.MarshalIndent(HeaderWrapper{Inner: MXHeaderData}, "", "\t")
	if err != nil {
		return "", fmt.Errorf("error marshaling MX header: %w", err)
	}

	//Body XML 추출
	/*
		payloadDecoded, err := base64.StdEncoding.DecodeString(message.Message.Payload)
		if err != nil {
			return "", fmt.Errorf("error decoding payload: %w", err)
		}
		body := string(payloadDecoded)
	*/
	body := message.Message.Payload
	bodyDoc, err := xmlquery.Parse(strings.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("error parsing body XML: %w", err)
	}
	bodyNode := xmlquery.FindOne(bodyDoc, "//*[local-name()='Envelope']").OutputXML(false)

	//MX 생성
	var stringBuilder strings.Builder
	stringBuilder.WriteString("<DataPDU><Revision>2.0.10</Revision>\n")
	stringBuilder.WriteString(string(headerBytes))
	stringBuilder.WriteString("\n")
	stringBuilder.WriteString(bodyNode)
	stringBuilder.WriteString("\n</DataPDU>")

	//format
	formattedString, err := fsutil.FormatXMLString(stringBuilder.String())
	if err != nil {
		return "", fmt.Errorf("error formatting XML: %w", err)
	}
	return formattedString, nil
}

func FINReportMaker(report MTReport) (string, error) {
	reportBuilder := strings.Builder{}
	//block 1
	reportBuilder.WriteString("{1:F21")
	//Sender BIC
	reportBuilder.WriteString(report.TransmissionReport.Message.Sender)
	//Session Number
	reportBuilder.WriteString(fmt.Sprintf("%04d", report.TransmissionReport.NetworkInfo.SessionNumber))
	//Sequence Number
	reportBuilder.WriteString(fmt.Sprintf("%06d", report.TransmissionReport.NetworkInfo.SequenceNumber))
	reportBuilder.WriteString("}")

	//block 4
	reportBuilder.WriteString("{4:")
	//177 block (Time)
	reportBuilder.WriteString("{177:")
	date, err := time.Parse("2006-01-02T15:04:05Z", report.TransmissionReport.ResponseDate)
	if err != nil {
		return "", fmt.Errorf("error parsing response date: %w", err)
	}
	reportBuilder.WriteString(date.Format("0601021504"))
	reportBuilder.WriteString("}")
	//451 block (status)
	reportBuilder.WriteString("{451:")
	switch report.TransmissionReport.DeliveryStatus {
	case "Acked":
		reportBuilder.WriteString("0}")
	case "Rejected":
		reportBuilder.WriteString("1}")
		reportBuilder.WriteString("{405:")
		reportBuilder.WriteString(report.TransmissionReport.RejectionCode)
		reportBuilder.WriteString("}")
	}
	//108 block
	reportBuilder.WriteString("{108:")
	reportBuilder.WriteString(report.TransmissionReport.SenderReference)
	reportBuilder.WriteString("}")
	//End of block 4
	reportBuilder.WriteString("}")

	return reportBuilder.String(), nil
}

func MXReportMaker(report MXReport) (string, error) {
	//Header
	var MXReportHeaderData MXReportHeader
	MXReportHeaderData.SenderReference = report.TransmissionReport.SenderReference
	MXReportHeaderData.NetworkDeliveryStatus = report.TransmissionReport.DeliveryStatus
	MXReportHeaderData.OriginalInstanceAddresseeX2 = strings.Split(strings.Split(report.TransmissionReport.Message.Requestor, ",")[0], "=")[1]
	MXReportHeaderData.OriginalInstanceAddresseeX1 = strings.ToUpper(strings.Split(strings.Split(report.TransmissionReport.Message.Requestor, ",")[1], "=")[1] + MXReportHeaderData.OriginalInstanceAddresseeX2)
	MXReportHeaderData.ReportingApplication = "SWIFTNetInterface"
	MXReportHeaderData.Priority = report.TransmissionReport.NetworkInfo.MessageNetworkPriority
	MXReportHeaderData.IsPossibleDuplicate = report.TransmissionReport.NetworkInfo.MessagePossibleDuplicate
	MXReportHeaderData.Service = report.TransmissionReport.ServiceCode
	MXReportHeaderData.Network = "SWIFTNet"
	MXReportHeaderData.SessionNr = report.TransmissionReport.NetworkInfo.SessionNumber
	MXReportHeaderData.SeqNr = report.TransmissionReport.NetworkInfo.SequenceNumber
	MXReportHeaderData.RequestSubtype = report.TransmissionReport.NetworkInfo.RequestSubtype
	MXReportHeaderData.RequestType = report.TransmissionReport.NetworkInfo.RequestType
	MXReportHeaderData.IntvCategory = "TransmissionReport"
	creationTime, err := time.Parse("2006-01-02T15:04:05Z", report.TransmissionReport.ResponseDate)
	if err != nil {
		return "", fmt.Errorf("error parsing response date: %w", err)
	}
	MXReportHeaderData.CreationTime = creationTime.Format("20060102150405")
	MXReportHeaderData.OperatorOrigin = "SYSTEM"
	reportBytes, err := base64.StdEncoding.DecodeString(report.TransmissionReport.Report)
	if err != nil {
		return "", fmt.Errorf("error decoding report: %w", err)
	}
	MXReportHeaderData.Contents = string(reportBytes)
	MXReportHeaderData.IsRelatedInstanceOriginal = true
	MXReportHeaderData.MessageCreator = "ApplicationInterface"
	MXReportHeaderData.IsMessageModified = false
	MXReportHeaderData.MessageFields = "HeaderAndBody"

	//Message
	MXReportHeaderData.Message.SenderReference = report.TransmissionReport.SenderReference
	MXReportHeaderData.Message.MessageIdentifier = report.TransmissionReport.Message.MessageType
	MXReportHeaderData.Message.Format = report.TransmissionReport.Message.Format
	if report.TransmissionReport.Message.Direction == "Incoming" {
		MXReportHeaderData.Message.SubFormat = "Output"
	} else {
		MXReportHeaderData.Message.SubFormat = "Input"
	}
	MXReportHeaderData.Message.SenderDN = report.TransmissionReport.Message.Requestor
	MXReportHeaderData.Message.SenderFullNameX1 = MXReportHeaderData.OriginalInstanceAddresseeX1
	MXReportHeaderData.Message.SenderFullNameX2 = MXReportHeaderData.OriginalInstanceAddresseeX2
	MXReportHeaderData.Message.ReceiverDN = report.TransmissionReport.Message.Responder
	MXReportHeaderData.Message.ReceiverFullNameX1 = strings.ToUpper(strings.Split(strings.Split(report.TransmissionReport.Message.Responder, ",")[1], "=")[1] + strings.Split(strings.Split(report.TransmissionReport.Message.Responder, ",")[0], "=")[1])
	MXReportHeaderData.Message.ReceiverFullNameX2 = strings.Split(strings.Split(report.TransmissionReport.Message.Responder, ",")[0], "=")[1]
	MXReportHeaderData.Message.InterfaceInfoCreator = "ApplicationInterface"
	MXReportHeaderData.Message.InterfaceInfoContext = "Report"
	MXReportHeaderData.Message.InterfaceInfoNature = "Financial"
	MXReportHeaderData.Message.NetworkInfoPriority = report.TransmissionReport.NetworkInfo.MessageNetworkPriority
	MXReportHeaderData.Message.NetworkInfoDuplicate = report.TransmissionReport.NetworkInfo.MessagePossibleDuplicate
	MXReportHeaderData.Message.NetworkInfoService = report.TransmissionReport.ServiceCode
	MXReportHeaderData.SessionNr = report.TransmissionReport.NetworkInfo.SessionNumber
	MXReportHeaderData.SeqNr = report.TransmissionReport.NetworkInfo.SequenceNumber
	MXReportHeaderData.Message.NetworkInfoSWIFTNetType = report.TransmissionReport.NetworkInfo.RequestType
	MXReportHeaderData.Message.NetworkInfoSWIFTNetSubtype = report.TransmissionReport.NetworkInfo.RequestSubtype

	//Header XML 생성
	type ReportHeaderWrapper struct {
		XMLName xml.Name       `xml:"Header"`
		Inner   MXReportHeader `xml:"TransmissionReport"`
	}
	headerBytes, err := xml.MarshalIndent(ReportHeaderWrapper{Inner: MXReportHeaderData}, "", "\t")
	if err != nil {
		return "", fmt.Errorf("error marshaling MX report header: %w", err)
	}

	//Body XML 생성 (Envelope 제거)
	/*
		payloadDecoded, err := base64.StdEncoding.DecodeString(report.TransmissionReport.Message.Payload)
		if err != nil {
			return "", fmt.Errorf("error decoding report: %w", err)
		}
		body := string(payloadDecoded)
	*/
	body := report.TransmissionReport.Message.Payload
	bodyDoc, err := xmlquery.Parse(strings.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("error parsing body XML: %w", err)
	}
	bodyNode := xmlquery.FindOne(bodyDoc, "//*[local-name()='Envelope']").OutputXML(false)

	//합치기
	finalReport := "<DataPDU><Revision>2.0.10</Revision>\n" + string(headerBytes) + "\n" + bodyNode + "</DataPDU>"
	formattedReport, err := fsutil.FormatXMLString(finalReport)
	if err != nil {
		return "", err
	}
	return formattedReport, nil
}
