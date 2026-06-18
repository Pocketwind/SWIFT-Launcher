package messaging

import (
	"os"

	"github.com/Pocketwind/SWIFT-Launcher/auth"
	"github.com/Pocketwind/SWIFT-Launcher/config"
	"github.com/Pocketwind/SWIFT-Launcher/fsutil"
	"github.com/Pocketwind/SWIFT-Launcher/logging"
)

func CollectorService(settings *config.Settings, partner *config.Partner, tokenData *auth.TokenData, logCh chan<- logging.LogData, exitCmd <-chan bool) {
	logging.Easylog(logCh, "INFO", "Starting Collector for partner: "+partner.Name)

loop:
	for {
		select {
		case filePath := <-partner.InputChannel:
			//파일 확장자 체크
			if fsutil.GetFileExt(filePath) != partner.Extension {
				logging.Easylog(logCh, "ERROR", "Skipping file with unsupported extension: "+filePath+" ("+partner.Name+")")
				errorPath := fsutil.PathHelper(partner.ErrorPath + "/" + fsutil.GetFileName(filePath))
				err := os.Rename(fsutil.PathHelper(filePath), errorPath)
				if err != nil {
					logging.Easylog(logCh, "ERROR", "Failed to move file to error directory: "+err.Error())
				}
				continue
			}
			//맞는 파일 처리
			logging.Easylog(logCh, "INFO", "Processing file: "+filePath+" ("+partner.Name+")")
			//in_progress로 이동
			progressPath := fsutil.PathHelper(partner.ProgressPath + "/" + fsutil.GetFileName(filePath))
			err := os.Rename(fsutil.PathHelper(filePath), progressPath)
			if err != nil {
				logging.Easylog(logCh, "ERROR", "Failed to move file to progress directory: "+err.Error())
				continue
			}
			switch partner.Type {
			case "interAct": //MX
				err := processMXFile(settings, progressPath, partner, tokenData, logCh)
				if err != nil {
					logging.Easylog(logCh, "ERROR", "Failed to process MX file: "+err.Error())
				}
			case "fin": //MT
				err := processMTFile(settings, progressPath, partner, tokenData, logCh)
				if err != nil {
					logging.Easylog(logCh, "ERROR", "Failed to process MT file: "+err.Error())
				}
			case "fileAct": //FileAct
			}
		case <-exitCmd:
			break loop
		}
	}
	logging.Easylog(logCh, "INFO", "Collector Stopped for partner: "+partner.Name)
}

func processMXFile(settings *config.Settings, filePath string, partner *config.Partner, tokenData *auth.TokenData, logCh chan<- logging.LogData) error {
	//MX 데이터 생성
	mxdata, err := MXDataMaker(filePath, logCh)
	if err != nil {
		logging.Easylog(logCh, "ERROR", "Failed to create MX data: "+err.Error())
		errorPath := fsutil.PathHelper(partner.ErrorPath + "/" + fsutil.GetFileName(filePath))
		err := os.Rename(fsutil.PathHelper(filePath), errorPath)
		if err != nil {
			logging.Easylog(logCh, "ERROR", "Failed to move file to error directory: "+err.Error())
		}
		return err
	}

	//send
	response, err := MXSender(mxdata, tokenData, settings, logCh)
	if err != nil {
		logging.Easylog(logCh, "ERROR", "Failed to send MX message: "+err.Error())
		errorPath := fsutil.PathHelper(partner.ErrorPath + "/" + fsutil.GetFileName(filePath))
		err := os.Rename(fsutil.PathHelper(filePath), errorPath)
		if err != nil {
			logging.Easylog(logCh, "ERROR", "Failed to move file to error directory: "+err.Error())
		}
		return err
	}

	//완료
	logging.Easylog(logCh, "INFO", "MX message sent successfully. Response: "+response)
	err = os.Remove(fsutil.PathHelper(filePath))
	if err != nil {
		logging.Easylog(logCh, "ERROR", "Failed to remove file: "+err.Error())
	}
	return nil
}

func processMTFile(settings *config.Settings, filePath string, partner *config.Partner, tokenData *auth.TokenData, logCh chan<- logging.LogData) error {
	//MT 데이터 생성
	mtdata, err := MTDataMaker(filePath, logCh)
	if err != nil {
		logging.Easylog(logCh, "ERROR", "Failed to create MT data: "+err.Error())
		errorPath := fsutil.PathHelper(partner.ErrorPath + "/" + fsutil.GetFileName(filePath))
		err := os.Rename(fsutil.PathHelper(filePath), errorPath)
		if err != nil {
			logging.Easylog(logCh, "ERROR", "Failed to move file to error directory: "+err.Error())
		}
		return err
	}

	//send
	response, err := MTSender(mtdata, tokenData, settings, logCh)
	if err != nil {
		logging.Easylog(logCh, "ERROR", "Failed to send MT message: "+err.Error())
		errorPath := fsutil.PathHelper(partner.ErrorPath + "/" + fsutil.GetFileName(filePath))
		err := os.Rename(fsutil.PathHelper(filePath), errorPath)
		if err != nil {
			logging.Easylog(logCh, "ERROR", "Failed to move file to error directory: "+err.Error())
		}
		return err
	}

	//완료
	logging.Easylog(logCh, "INFO", "MT message sent successfully. Response: "+response)
	err = os.Remove(fsutil.PathHelper(filePath))
	if err != nil {
		logging.Easylog(logCh, "ERROR", "Failed to remove file: "+err.Error())
	}
	return nil
}
