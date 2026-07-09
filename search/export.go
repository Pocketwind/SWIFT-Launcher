package search

import (
	"fmt"
	"os"

	"github.com/Pocketwind/SWIFT-Launcher/messaging"
)

func ExportMessages(args []string) error {
	options, err := ParseSearchArgs(args)
	if err != nil {
		return err
	}

	//DB 연결
	db, err := getMessageDB()
	if err != nil {
		println("Error opening database:", err.Error())
		return err
	}

	//tx
	tx, err := db.Begin()
	if err != nil {
		println("Error starting transaction:", err.Error())
		return err
	}
	defer tx.Rollback()

	//검색
	results, err := searchMessages(tx, options)
	if err != nil {
		println("Error searching messages:", err.Error())
		return err
	}

	//export
	for _, msg := range results {
		switch msg.Service {
		case "fin":
			err := exportMT(msg, options.Path)
			if err != nil {
				println("Error exporting message:", err.Error())
				return err
			}
		case "interAct":
			err := exportMX(msg, options.Path)
			if err != nil {
				println("Error exporting message:", err.Error())
				return err
			}
		case "fileAct":
		default:
			println("Unsupported service type for export:", msg.Service)
			continue
		}
	}
	return nil
}

func exportMT(msg SearchResult, exportDir string) error {
	//MT 메시지 export
	fileName := fmt.Sprintf("%d.txt", msg.DistributionID)
	filePath := exportDir + "/" + fileName
	distribution := messaging.Distribution{
		ID:                int(msg.DistributionID),
		Service:           msg.Service,
		DistributionTag:   msg.Tag,
		PossibleDuplicate: msg.PossibleDuplicate == "1",
		CloudReference:    msg.CloudReference,
	}
	message := messaging.MTMessage{
		SenderReference: msg.SenderReference,
		MessageType:     msg.MessageType,
		Sender:          msg.Sender,
		Receiver:        msg.Receiver,
		Direction:       msg.Direction,
		Payload:         msg.RawText,
		NetworkInfo: messaging.NetworkInfo{
			NetworkPriority: msg.Priority,
		},
	}
	if message.Payload == "" {
		return fmt.Errorf("empty MT payload for distribution %d", msg.DistributionID)
	}

	mtData := messaging.MTDownload{
		Distribution: distribution,
		Message:      message,
	}
	content, err := messaging.FINMessageMaker(mtData)
	if err != nil {
		println("Error creating FIN message:", err.Error())
		return err
	}

	err = os.WriteFile(filePath, []byte(content), 0644)
	if err != nil {
		println("Error writing FIN message to file:", err.Error())
		return err
	}

	return nil
}

func exportMX(msg SearchResult, exportDir string) error {
	fileName := fmt.Sprintf("%d.txt", msg.DistributionID)
	filePath := exportDir + "/" + fileName
	distribution := messaging.Distribution{
		ID:                int(msg.DistributionID),
		Service:           msg.Service,
		DistributionTag:   msg.Tag,
		PossibleDuplicate: msg.PossibleDuplicate == "1",
		CloudReference:    msg.CloudReference,
	}
	message := messaging.MXMessage{
		SenderReference: msg.SenderReference,
		MessageType:     msg.MessageType,
		Requestor:       msg.Sender,
		Responder:       msg.Receiver,
		Direction:       msg.Direction,
		Payload:         msg.RawText,
		NetworkInfo: messaging.NetworkInfo{
			NetworkPriority: msg.Priority,
		},
	}
	if message.Payload == "" {
		return fmt.Errorf("empty MX payload for distribution %d", msg.DistributionID)
	}
	mxData := messaging.MXDownload{
		Distribution: distribution,
		Message:      message,
	}
	content, err := messaging.MXMessageMaker(mxData)
	if err != nil {
		println("Error creating MX message:", err.Error())
		return err
	}

	err = os.WriteFile(filePath, []byte(content), 0644)
	if err != nil {
		println("Error writing MX message to file:", err.Error())
		return err
	}

	return nil
}
