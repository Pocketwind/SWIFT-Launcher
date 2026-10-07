package messaging

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Pocketwind/SWIFT-Launcher/auth"
	"github.com/Pocketwind/SWIFT-Launcher/config"
	"github.com/Pocketwind/SWIFT-Launcher/fsutil"
	"github.com/Pocketwind/SWIFT-Launcher/logging"
)

func CollectorService(settings *config.Settings, partner *config.Partner, tokenData *auth.TokenData, logCh chan<- logging.LogData, exitCmd <-chan bool) {
	if !partner.Status {
		logging.Easylog(logCh, "INFO", "Collector is disabled for partner: "+partner.Name)
		return
	}
	logging.Easylog(logCh, "INFO", "Starting Collector for partner: "+partner.Name)
	defer logging.Easylog(logCh, "INFO", "Collector stopped for partner: "+partner.Name)
	for {
		// Prioritize shutdown even if many inputs are queued.
		select {
		case <-exitCmd:
			return
		default:
		}
		select {
		case <-exitCmd:
			return
		case filePath, ok := <-partner.InputChannel:
			if !ok {
				return
			}
			select {
			case <-exitCmd:
				return
			default:
			}
			if fsutil.GetFileExt(filePath) != partner.Extension {
				continue
			}
			if fsutil.IsPathUnderDir(filePath, partner.ProgressPath) {
				logging.Easylog(logCh, "WARN", "Held progress file requires remote reconciliation before resubmission: "+filePath)
				continue
			}
			if !fsutil.IsPathUnderDir(filePath, partner.InputPath) {
				logging.Easylog(logCh, "ERROR", "Input file is outside the configured input directory: "+filePath)
				continue
			}
			progressPath := filepath.Join(partner.ProgressPath, fsutil.GetFileName(filePath))
			// A previous unresolved attempt must never be overwritten.
			if _, err := os.Lstat(progressPath); !os.IsNotExist(err) {
				logging.Easylog(logCh, "ERROR", "Progress destination already exists or cannot be checked; input held: "+progressPath)
				continue
			}
			if err := os.Rename(fsutil.PathHelper(filePath), fsutil.PathHelper(progressPath)); err != nil {
				logging.Easylog(logCh, "ERROR", "Failed to claim input file: "+err.Error())
				continue
			}
			logging.Easylog(logCh, "INFO", "Processing file: "+progressPath+" ("+partner.Name+")")
			var err error
			switch partner.Type {
			case "interAct":
				err = processMXFile(settings, progressPath, partner, tokenData, logCh, false)
			case "fin":
				err = processMTFile(settings, progressPath, partner, tokenData, logCh, false)
			case "fileAct":
				if partner.IsDFA {
					err = processDFAFile(settings, progressPath, partner, tokenData, logCh, false)
				} else {
					err = processFileActFile(settings, progressPath, partner, tokenData, logCh, false)
				}
			default:
				err = routeSendFailure(progressPath, partner, fmt.Errorf("unsupported partner type %q", partner.Type))
			}
			if err != nil {
				logging.Easylog(logCh, "ERROR", "Failed to process outgoing file: "+err.Error())
			}
		}
	}
}

func routeSendFailure(filePath string, partner *config.Partner, err error) error {
	var uncertain *UncertainSendError
	if errors.As(err, &uncertain) {
		// Progress is never automatically re-enqueued on restart. Persist the
		// reason alongside the untouched source for operator reconciliation.
		metadata, _ := json.MarshalIndent(map[string]any{
			"state": "uncertain", "error": err.Error(), "time": time.Now().UTC().Format(time.RFC3339),
		}, "", "  ")
		if writeErr := fsutil.AtomicWriteFile(filePath+".hold.json", metadata, 0600); writeErr != nil {
			return fmt.Errorf("%w; failed to record held state: %v", err, writeErr)
		}
		return err
	}
	if routeErr := ErrorMessageRouter(filePath, partner); routeErr != nil {
		return fmt.Errorf("%w; %v", err, routeErr)
	}
	return err
}

func archiveSentFile(filePath string, partner *config.Partner, reference string, payload ...[]byte) (string, error) {
	sentDir := filepath.Join(partner.ProgressPath, "sent")
	if err := os.MkdirAll(sentDir, 0700); err != nil {
		return "", err
	}
	archiveDir, err := os.MkdirTemp(sentDir, time.Now().UTC().Format("20060102T150405Z")+"-*")
	if err != nil {
		return "", err
	}
	originalDir := filepath.Join(archiveDir, "original")
	if err := os.Mkdir(originalDir, 0700); err != nil {
		return "", err
	}
	// A normal FileAct companion and its Body are separate files. Preserve
	// the validated body bytes as well, without deleting a possibly shared
	// input payload that another companion still references.
	if len(payload) != 0 {
		if err := fsutil.AtomicWriteFile(filepath.Join(archiveDir, "payload.bin"), payload[0], 0600); err != nil {
			return "", err
		}
	}
	receipt, err := json.MarshalIndent(map[string]any{
		"state": "submitted", "reference": reference, "partner": partner.Name,
		"file": filepath.Base(filePath), "time": time.Now().UTC().Format(time.RFC3339),
	}, "", "  ")
	if err != nil {
		return "", err
	}
	// Record remote acceptance before moving the source. If either write or
	// move fails, the source stays held in progress rather than being retried.
	if err := fsutil.AtomicWriteFile(filepath.Join(archiveDir, "receipt.json"), receipt, 0600); err != nil {
		return "", err
	}
	if err := os.Rename(fsutil.PathHelper(filePath), filepath.Join(originalDir, filepath.Base(filePath))); err != nil {
		return "", err
	}
	return archiveDir, nil
}

func completeOutgoing(filePath string, partner *config.Partner, reference string, logCh chan<- logging.LogData, payload ...[]byte) error {
	archive, err := archiveSentFile(filePath, partner, reference, payload...)
	if err != nil {
		return routeSendFailure(filePath, partner, uncertainSend("accepted submission "+reference, fmt.Errorf("archiving original: %w", err)))
	}
	logging.Easylog(logCh, "INFO", "Message submitted successfully. Reference: "+reference+"; original archived: "+archive)
	return nil
}

func processFileActFile(settings *config.Settings, filePath string, partner *config.Partner, token *auth.TokenData, logCh chan<- logging.LogData, isPDE bool) error {
	data, err := FileActDataMaker(filePath, partner, logCh)
	if err != nil {
		return routeSendFailure(filePath, partner, fmt.Errorf("creating FileAct data: %w", err))
	}
	payload, err := loadFileActPayload(data, filePath, partner)
	if err != nil {
		return routeSendFailure(filePath, partner, err)
	}
	reference, err := FileActSender(data, filePath, token, partner, settings, logCh, isPDE)
	if err != nil {
		return routeSendFailure(filePath, partner, err)
	}
	return completeOutgoing(filePath, partner, reference, logCh, payload)
}

func processMXFile(settings *config.Settings, filePath string, partner *config.Partner, token *auth.TokenData, logCh chan<- logging.LogData, isPDE bool) error {
	data, err := MXDataMaker(filePath, logCh)
	if err != nil {
		return routeSendFailure(filePath, partner, fmt.Errorf("creating MX data: %w", err))
	}
	reference, err := MXSender(data, token, settings, logCh, isPDE)
	if err != nil {
		return routeSendFailure(filePath, partner, err)
	}
	return completeOutgoing(filePath, partner, reference, logCh)
}

func processMTFile(settings *config.Settings, filePath string, partner *config.Partner, token *auth.TokenData, logCh chan<- logging.LogData, isPDE bool) error {
	data, err := MTDataMaker(filePath, logCh)
	if err != nil {
		return routeSendFailure(filePath, partner, fmt.Errorf("creating MT data: %w", err))
	}
	reference, err := MTSender(data, token, settings, logCh, isPDE)
	if err != nil {
		return routeSendFailure(filePath, partner, err)
	}
	return completeOutgoing(filePath, partner, reference, logCh)
}

func processDFAFile(settings *config.Settings, filePath string, partner *config.Partner, token *auth.TokenData, logCh chan<- logging.LogData, isPDE bool) error {
	data, err := DFADataMaker(filePath, partner, logCh)
	if err != nil {
		return routeSendFailure(filePath, partner, fmt.Errorf("creating DFA data: %w", err))
	}
	reference, err := FileActSender(data, filePath, token, partner, settings, logCh, isPDE)
	if err != nil {
		return routeSendFailure(filePath, partner, err)
	}
	return completeOutgoing(filePath, partner, reference, logCh)
}
