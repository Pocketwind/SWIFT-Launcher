package messaging

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Pocketwind/SWIFT-Launcher/config"
)

// archiveSentFile preserves each accepted input under a unique timestamp
// directory. Copying before removal also permits a different archive volume.
func archiveSentFile(filePath string, partner *config.Partner, reference string, payload ...[]byte) (archivedPath string, resultErr error) {
	source, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("opening original: %w", err)
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil {
		return "", fmt.Errorf("checking original: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("original is not a regular file: %s", filePath)
	}

	name := filepath.Base(filePath)
	if strings.EqualFold(name, "receipt.json") || len(payload) != 0 && strings.EqualFold(name, "payload.bin") {
		return "", fmt.Errorf("reserved archive filename: %s", name)
	}
	archiveRoot := partner.ArchiveDirectory()
	if err := os.MkdirAll(archiveRoot, 0700); err != nil {
		return "", fmt.Errorf("creating archive directory: %w", err)
	}
	now := time.Now().UTC()
	archiveDir, err := createArchiveDirectory(archiveRoot, now)
	if err != nil {
		return "", fmt.Errorf("creating timestamp archive directory: %w", err)
	}
	destination := filepath.Join(archiveDir, name)
	var created []string
	complete := false
	defer func() {
		if resultErr == nil || complete {
			return
		}
		// Every entry was created exclusively by this attempt. Existing archive
		// files and the progress original are never part of this cleanup.
		for _, path := range created {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				resultErr = errors.Join(resultErr, fmt.Errorf("removing incomplete archive %s: %w", path, err))
			}
		}
		if err := os.Remove(archiveDir); err != nil && !os.IsNotExist(err) {
			resultErr = errors.Join(resultErr, fmt.Errorf("removing incomplete archive directory %s: %w", archiveDir, err))
		}
	}()

	original, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", fmt.Errorf("creating archived original: %w", err)
	}
	created = append(created, destination)
	_, copyErr := io.Copy(original, source)
	closeErr := source.Close()
	if err := errors.Join(finishArchiveFile(original, copyErr), closeErr); err != nil {
		return "", fmt.Errorf("copying archived original: %w", err)
	}
	writeSidecar := func(name string, data []byte) error {
		path := filepath.Join(archiveDir, name)
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		created = append(created, path)
		_, writeErr := file.Write(data)
		return finishArchiveFile(file, writeErr)
	}
	if len(payload) != 0 {
		if err := writeSidecar("payload.bin", payload[0]); err != nil {
			return "", fmt.Errorf("archiving FileAct payload: %w", err)
		}
	}
	receipt, err := json.MarshalIndent(map[string]any{
		"state": "submitted", "reference": reference, "partner": partner.Name,
		"file": name, "archive_file": filepath.Base(destination), "time": now.Format(time.RFC3339Nano),
	}, "", "  ")
	if err != nil {
		return "", fmt.Errorf("creating archive receipt: %w", err)
	}
	if err := writeSidecar("receipt.json", receipt); err != nil {
		return "", fmt.Errorf("archiving receipt: %w", err)
	}
	complete = true
	// All archive files are synced and closed before relinquishing the source.
	if err := os.Remove(filePath); err != nil {
		// Keep the complete archive and receipt as reconciliation evidence.
		return "", fmt.Errorf("archive %s is complete but removing progress original failed: %w", destination, err)
	}
	return destination, nil
}

func createArchiveDirectory(root string, now time.Time) (string, error) {
	return createTimestampDirectory(root, "", now)
}

func createTimestampDirectory(root, prefix string, now time.Time) (string, error) {
	timestamp := now.UTC().Format("20060102T150405.000000000Z")
	for suffix := 0; ; suffix++ {
		name := prefix + timestamp
		if suffix != 0 {
			name += fmt.Sprintf("-%d", suffix)
		}
		dir := filepath.Join(root, name)
		if err := os.Mkdir(dir, 0700); err != nil {
			if os.IsExist(err) {
				continue
			}
			return "", err
		}
		return dir, nil
	}
}

func finishArchiveFile(file *os.File, writeErr error) error {
	if writeErr != nil {
		return errors.Join(writeErr, file.Close())
	}
	return errors.Join(file.Sync(), file.Close())
}
