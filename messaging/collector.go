package messaging

import (
	"fmt"
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
				/*
					errorPath := fsutil.PathHelper(partner.ErrorPath + "/" + fsutil.GetFileName(filePath))
					err := os.Rename(fsutil.PathHelper(filePath), errorPath)
					if err != nil {
						logging.Easylog(logCh, "ERROR", "Failed to move file to error directory: "+err.Error())
					}
				*/
				continue
			}
			//맞는 파일 처리
			logging.Easylog(logCh, "INFO", "Processing file: "+filePath+" ("+partner.Name+")")
			//PDE 체크(progress에 있으면 PDE붙이기)
			isPDE := fsutil.IsPathUnderDir(filePath, partner.ProgressPath)
			//in_progress로 이동
			progressPath := fsutil.PathHelper(partner.ProgressPath + "/" + fsutil.GetFileName(filePath))
			err := os.Rename(fsutil.PathHelper(filePath), fsutil.PathHelper(progressPath))
			if err != nil {
				logging.Easylog(logCh, "ERROR", "Failed to move file to progress directory: "+err.Error())
				continue
			}
			switch partner.Type {
			case "interAct": //MX
				err := processMXFile(settings, progressPath, partner, tokenData, logCh, isPDE)
				if err != nil {
					logging.Easylog(logCh, "ERROR", "Failed to process MX file: "+err.Error())
				}
			case "fin": //MT
				err := processMTFile(settings, progressPath, partner, tokenData, logCh, isPDE)
				if err != nil {
					logging.Easylog(logCh, "ERROR", "Failed to process MT file: "+err.Error())
				}
			case "fileAct": //FileAct
				if partner.IsDFA { //DFA
					err := processDFAFile(settings, progressPath, partner, tokenData, logCh, isPDE)
					if err != nil {
						logging.Easylog(logCh, "ERROR", "Failed to process DFA file: "+err.Error())
					}
				} else { //일반 FA
					err := processFileActFile(settings, progressPath, partner, tokenData, logCh, isPDE)
					if err != nil {
						logging.Easylog(logCh, "ERROR", "Failed to process FileAct file: "+err.Error())
					}
				}
			}
		case <-exitCmd:
			break loop
		}
	}
	logging.Easylog(logCh, "INFO", "Collector Stopped for partner: "+partner.Name)
}

func processFileActFile(settings *config.Settings, filePath string, partner *config.Partner, tokenData *auth.TokenData, logCh chan<- logging.LogData, isPDE bool) error {
	//FileAct 데이터 생성
	fadata, err := FileActDataMaker(filePath, partner, logCh)
	if err != nil {
		ferr := ErrorMessageRouter(filePath, partner)
		if ferr != nil {
			err = fmt.Errorf("%s; %s", err.Error(), ferr.Error())
		}
		return fmt.Errorf("failed to create FileAct data: %w", err)
	}

	//send
	response, err := FileActSender(fadata, filePath, tokenData, partner, settings, logCh, isPDE)
	if err != nil {
		ferr := ErrorMessageRouter(filePath, partner)
		if ferr != nil {
			err = fmt.Errorf("%s; %s", err.Error(), ferr.Error())
		}
		return err
	}

	//완료
	if isPDE {
		logging.Easylog(logCh, "WARN", "FileAct message sent successfully with PDE. Response: "+response)
	} else {
		logging.Easylog(logCh, "INFO", "FileAct message sent successfully. Response: "+response)
	}
	err = os.Remove(fsutil.PathHelper(filePath))
	if err != nil {
		return fmt.Errorf("failed to remove file: %w", err)
	}
	return nil
}

func processMXFile(settings *config.Settings, filePath string, partner *config.Partner, tokenData *auth.TokenData, logCh chan<- logging.LogData, isPDE bool) error {
	//MX 데이터 생성
	mxdata, err := MXDataMaker(filePath, logCh)
	if err != nil {
		ferr := ErrorMessageRouter(filePath, partner)
		if ferr != nil {
			err = fmt.Errorf("%s; %s", err.Error(), ferr.Error())
		}
		return fmt.Errorf("failed to create MX data: %w", err)
	}

	//send
	response, err := MXSender(mxdata, tokenData, settings, logCh, isPDE)
	if err != nil {
		ferr := ErrorMessageRouter(filePath, partner)
		if ferr != nil {
			err = fmt.Errorf("%s; %s", err.Error(), ferr.Error())
		}
		return fmt.Errorf("failed to send MX message: %w", err)
	}

	//완료
	if isPDE {
		logging.Easylog(logCh, "WARN", "MX message sent successfully with PDE. Response: "+response)
	} else {
		logging.Easylog(logCh, "INFO", "MX message sent successfully. Response: "+response)
	}
	err = os.Remove(fsutil.PathHelper(filePath))
	if err != nil {
		return fmt.Errorf("failed to remove file: %w", err)
	}
	return nil
}

func processMTFile(settings *config.Settings, filePath string, partner *config.Partner, tokenData *auth.TokenData, logCh chan<- logging.LogData, isPDE bool) error {
	//MT 데이터 생성
	mtdata, err := MTDataMaker(filePath, logCh)
	if err != nil {
		ferr := ErrorMessageRouter(filePath, partner)
		if ferr != nil {
			err = fmt.Errorf("%s; %s", err.Error(), ferr.Error())
		}
		return fmt.Errorf("failed to create MT data: %w", err)
	}

	//send
	response, err := MTSender(mtdata, tokenData, settings, logCh, isPDE)
	if err != nil {
		ferr := ErrorMessageRouter(filePath, partner)
		if ferr != nil {
			err = fmt.Errorf("%s; %s", err.Error(), ferr.Error())
		}
		return fmt.Errorf("failed to send MT message: %w", err)
	}

	//완료
	if isPDE {
		logging.Easylog(logCh, "WARN", "MT message sent successfully with PDE. Response: "+response)
	} else {
		logging.Easylog(logCh, "INFO", "MT message sent successfully. Response: "+response)
	}
	err = os.Remove(fsutil.PathHelper(filePath))
	if err != nil {
		return fmt.Errorf("failed to remove file: %w", err)
	}
	return nil
}
func processDFAFile(settings *config.Settings, filePath string, partner *config.Partner, tokenData *auth.TokenData, logCh chan<- logging.LogData, isPDE bool) error {
	//DFA 데이터 생성
	fadata, err := DFADataMaker(filePath, partner, logCh)
	if err != nil {
		ferr := ErrorMessageRouter(filePath, partner)
		if ferr != nil {
			err = fmt.Errorf("%s; %s", err.Error(), ferr.Error())
		}
		return fmt.Errorf("failed to create DFA data: %w", err)
	}
	//logging.Easylog(logCh, "INFO", fmt.Sprintf("fadata: %+v", fadata))

	//send
	response, err := FileActSender(fadata, filePath, tokenData, partner, settings, logCh, isPDE)
	if err != nil {
		ferr := ErrorMessageRouter(filePath, partner)
		if ferr != nil {
			err = fmt.Errorf("%s; %s", err.Error(), ferr.Error())
		}
		return fmt.Errorf("failed to send FileAct message: %w", err)
	}
	//logging.Easylog(logCh, "INFO", fmt.Sprintf("fadata: %+v", fadata))

	//다했으면 파일 지우기(Body)
	err = os.Remove(fsutil.PathHelper(filePath))
	if err != nil {
		return fmt.Errorf("failed to delete file: %w", err)
	}

	//완료
	if isPDE {
		logging.Easylog(logCh, "WARN", "DFA file transfer completed successfully with PDE. Response: "+response)
	} else {
		logging.Easylog(logCh, "INFO", "DFA file transfer completed successfully. Response: "+response)
	}

	return nil
}
