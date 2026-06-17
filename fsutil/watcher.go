package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Pocketwind/SWIFT-Launcher/config"
	"github.com/Pocketwind/SWIFT-Launcher/logging"
	"github.com/fsnotify/fsnotify"
)

func WatchFileService(partner *config.Partner, exitCmd <-chan bool, logCh chan<- logging.LogData) {
	logging.Easylog(logCh, "INFO", fmt.Sprintf("Starting File Watcher Service for path: %s", partner.InputPath))

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error creating watcher: %v", err))
		return
	}
	defer watcher.Close()

	err = watcher.Add(partner.InputPath)
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error adding file to watcher: %v", err))
		return
	}

	// Process files that were already present while the program was down.
	enqueueExistingFiles(partner, exitCmd, logCh)

loop:
	for {
		select {
		case event := <-watcher.Events:
			if event.Op&fsnotify.Create == fsnotify.Create {
				info, err := os.Stat(event.Name)
				if err != nil {
					continue
				}
				if info.IsDir() {
					continue
				}
				//event.Name이 파일 들어온 경로
				WaitFileReady(event.Name, 10*time.Second) //파일이 완전히 쓰여질 때까지 대기
				select {
				case partner.InputChannel <- event.Name:
				case <-exitCmd:
					break loop
				}
			}
		case err := <-watcher.Errors:
			if err != nil {
				logging.Easylog(logCh, "ERROR", fmt.Sprintf("File watcher error: %v", err))
			}
		case <-exitCmd:
			break loop
		}
	}

	logging.Easylog(logCh, "INFO", "File Watcher Service stopped")
}

func enqueueExistingFiles(partner *config.Partner, exitCmd <-chan bool, logCh chan<- logging.LogData) {
	//input 쌓인거 처리
	entries, err := os.ReadDir(partner.InputPath)
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error reading startup files: %v", err))
		return
	}

	logging.Easylog(logCh, "INFO", fmt.Sprintf("Startup scan started: %s", partner.InputPath))
	queued := 0

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		fullPath := filepath.Join(partner.InputPath, entry.Name())
		if err := WaitFileReady(fullPath, 10*time.Second); err != nil {
			logging.Easylog(logCh, "ERROR", fmt.Sprintf("Startup file is not ready: %s: %v", fullPath, err))
			continue
		}

		select {
		case partner.InputChannel <- fullPath:
			queued++
			logging.Easylog(logCh, "INFO", fmt.Sprintf("Queued startup file: %s", fullPath))
		case <-exitCmd:
			return
		}
	}

	logging.Easylog(logCh, "INFO", fmt.Sprintf("Startup scan completed: %s (queued=%d)", partner.InputPath, queued))

	//progress 쌓인거 처리
	entries, err = os.ReadDir(partner.ProgressPath)
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error reading startup files: %v", err))
		return
	}

	logging.Easylog(logCh, "INFO", fmt.Sprintf("Startup scan started: %s", partner.ProgressPath))
	queued = 0

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		fullPath := filepath.Join(partner.ProgressPath, entry.Name())
		if err := WaitFileReady(fullPath, 10*time.Second); err != nil {
			logging.Easylog(logCh, "ERROR", fmt.Sprintf("Startup file is not ready: %s: %v", fullPath, err))
			continue
		}
		select {
		case partner.InputChannel <- fullPath:
			queued++
			logging.Easylog(logCh, "INFO", fmt.Sprintf("Queued startup file: %s", fullPath))
		case <-exitCmd:
			return
		}
	}

	logging.Easylog(logCh, "INFO", fmt.Sprintf("Startup scan completed: %s (queued=%d)", partner.ProgressPath, queued))
}
