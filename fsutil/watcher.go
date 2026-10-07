package fsutil

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Pocketwind/SWIFT-Launcher/config"
	"github.com/Pocketwind/SWIFT-Launcher/logging"
	"github.com/fsnotify/fsnotify"
)

func watcherInputFile(path string, partner *config.Partner) bool {
	name := filepath.Base(path)
	if strings.HasPrefix(name, ".swift-") && strings.HasSuffix(name, ".tmp") {
		return false
	}
	return filepath.Ext(path) == partner.Extension
}

func watcherContext(exitCmd <-chan bool) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		select {
		case <-exitCmd:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}

func WatchFileService(partner *config.Partner, exitCmd <-chan bool, logCh chan<- logging.LogData) {
	if !partner.Status {
		return
	}
	logging.Easylog(logCh, "INFO", fmt.Sprintf("Starting File Watcher Service for path: %s", partner.InputPath))
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error creating watcher: %v", err))
		return
	}
	defer watcher.Close()
	if err := watcher.Add(partner.InputPath); err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error adding directory to watcher: %v", err))
		return
	}
	ctx, cancel := watcherContext(exitCmd)
	defer cancel()
	defer logging.Easylog(logCh, "INFO", "File Watcher Service stopped")
	queuedFiles := make(map[string]os.FileInfo)
	if partner.Direction == "in" {
		queuedFiles = enqueueExistingFilesContext(ctx, partner, logCh)
	}
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if event.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
				delete(queuedFiles, filepath.Clean(event.Name))
			}
			// Writes can recover an earlier ready timeout. Queue each file
			// identity once so delayed writes cannot create stale path retries.
			if event.Op&(fsnotify.Create|fsnotify.Write) == 0 || !watcherInputFile(event.Name, partner) {
				continue
			}
			info, err := os.Stat(event.Name)
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			pathKey := filepath.Clean(event.Name)
			if previous, ok := queuedFiles[pathKey]; ok && os.SameFile(previous, info) {
				continue
			}
			if err := WaitFileReadyContext(ctx, event.Name, 10*time.Second); err != nil {
				if ctx.Err() != nil {
					return
				}
				logging.Easylog(logCh, "ERROR", fmt.Sprintf("File is not ready: %s: %v", event.Name, err))
				continue
			}
			info, err = os.Stat(event.Name)
			if err != nil {
				continue
			}
			select {
			case partner.InputChannel <- event.Name:
				queuedFiles[pathKey] = info
			case <-ctx.Done():
				return
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			if err != nil {
				logging.Easylog(logCh, "ERROR", fmt.Sprintf("File watcher error: %v", err))
			}
		case <-ctx.Done():
			return
		}
	}
}

func enqueueExistingFiles(partner *config.Partner, exitCmd <-chan bool, logCh chan<- logging.LogData) {
	ctx, cancel := watcherContext(exitCmd)
	defer cancel()
	enqueueExistingFilesContext(ctx, partner, logCh)
}

func enqueueExistingFilesContext(ctx context.Context, partner *config.Partner, logCh chan<- logging.LogData) map[string]os.FileInfo {
	queuedFiles := make(map[string]os.FileInfo)
	// A progress file may have reached the server before the launcher exited.
	// PDE does not guarantee deduplication; leave all such files for review.
	progressEntries, err := os.ReadDir(partner.ProgressPath)
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error reading held progress files: %v", err))
	} else {
		for _, entry := range progressEntries {
			if !entry.IsDir() && watcherInputFile(entry.Name(), partner) {
				logging.Easylog(logCh, "WARN", "Progress file held; verify remote status before resubmitting: "+filepath.Join(partner.ProgressPath, entry.Name()))
			}
		}
	}
	entries, err := os.ReadDir(partner.InputPath)
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error reading startup input files: %v", err))
		return queuedFiles
	}
	queued := 0
	for _, entry := range entries {
		if ctx.Err() != nil {
			return queuedFiles
		}
		if entry.IsDir() || !watcherInputFile(entry.Name(), partner) {
			continue
		}
		fullPath := filepath.Join(partner.InputPath, entry.Name())
		if err := WaitFileReadyContext(ctx, fullPath, 10*time.Second); err != nil {
			if ctx.Err() != nil {
				return queuedFiles
			}
			logging.Easylog(logCh, "ERROR", fmt.Sprintf("Startup file is not ready: %s: %v", fullPath, err))
			continue
		}
		info, err := os.Stat(fullPath)
		if err != nil {
			continue
		}
		select {
		case partner.InputChannel <- fullPath:
			queued++
			queuedFiles[filepath.Clean(fullPath)] = info
		case <-ctx.Done():
			return queuedFiles
		}
	}
	logging.Easylog(logCh, "INFO", fmt.Sprintf("Startup input scan completed: %s (queued=%d)", partner.InputPath, queued))
	return queuedFiles
}
