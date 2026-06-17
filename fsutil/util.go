package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/antchfx/xmlquery"
	"github.com/beevik/etree"
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

func GetFileName(path string) string {
	if path == "" {
		return ""
	}
	return filepath.Base(path)
}

func GetFileExt(path string) string {
	if path == "" {
		return ""
	}
	return filepath.Ext(path)
}

func SafeInnerText(node *xmlquery.Node) string {
	if node == nil {
		return ""
	}
	return node.InnerText()
}

func FormatXMLString(raw string) (string, error) {
	doc := etree.NewDocument()
	if err := doc.ReadFromString(raw); err != nil {
		return "", fmt.Errorf("error parsing XML for formatting: %w", err)
	}

	doc.IndentTabs()

	formatted, err := doc.WriteToString()
	if err != nil {
		return "", fmt.Errorf("error writing formatted XML: %w", err)
	}

	return formatted, nil
}
