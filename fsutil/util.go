package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func EnsureDir(dirs ...string) error {
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("error creating directory %s: %w", dir, err)
		}
	}
	return nil
}

func WaitFileReady(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var prevSize int64 = -1

	for time.Now().Before(deadline) {
		info, err := os.Stat(path)
		if err != nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		size := info.Size()
		if size > 0 && size == prevSize {
			return nil // 크기 변화 없음 -> 쓰기 완료
		}
		prevSize = size
		time.Sleep(100 * time.Millisecond)
	}

	return fmt.Errorf("timeout waiting for file ready: %s", path)
}

func ErrorMessageRouter(prev string) error {
	filePath := filepath.Dir(prev)
	baseName := filepath.Base(prev)
	if err := os.MkdirAll(filePath+"/error", 0755); err != nil {
		return err
	}

	newPath := filePath + "/error/" + baseName
	if err := os.Rename(prev, newPath); err != nil {
		return err
	}
	return nil
}

func PathHelper(path string) string {
	if path == "" {
		return ""
	}
	return strings.ReplaceAll(path, "\\", "/")
}
