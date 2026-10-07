package fsutil

import (
	"bytes"
	"context"
	"fmt"
	"io"
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
	return WaitFileReadyContext(context.Background(), path, timeout)
}

func WaitFileReadyContext(parent context.Context, path string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var prevSize int64 = -1
	var prevMod time.Time
	var stableSince time.Time
	// Only a fallback; producers should rename completed temporary files.
	const stableWindow = time.Second
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("waiting for file %s: %w", path, err)
		}
		info, err := os.Stat(path)
		if err != nil {
			prevSize = -1
			stableSince = time.Time{}
		} else {
			if !info.Mode().IsRegular() {
				return fmt.Errorf("not a regular file: %s", path)
			}
			size := info.Size()
			if size > 0 && size == prevSize && info.ModTime().Equal(prevMod) {
				if time.Since(stableSince) >= stableWindow {
					return nil
				}
			} else {
				stableSince = time.Now()
			}
			prevSize, prevMod = size, info.ModTime()
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for file %s: %w", path, ctx.Err())
		case <-ticker.C:
		}
	}
}

func AtomicWriteFile(path string, data []byte, mode os.FileMode) error {
	return AtomicWriteReader(path, bytes.NewReader(data), mode)
}

// AtomicWriteReader keeps incomplete content out of the final consumer path.
func AtomicWriteReader(path string, reader io.Reader, mode os.FileMode) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".swift-*.tmp")
	if err != nil {
		return err
	}
	tempPath := file.Name()
	defer os.Remove(tempPath)
	defer file.Close()
	if err := file.Chmod(mode); err != nil {
		return err
	}
	if _, err := io.Copy(file, reader); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}

func PathHelper(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	// Preserve parent segments and UNC roots using native path semantics.
	return filepath.ToSlash(filepath.Clean(filepath.FromSlash(strings.ReplaceAll(path, "\\", "/"))))
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

// 비어있는 map 값 재귀로 제거하기
func RemoveEmptyValues(m map[string]interface{}) map[string]interface{} {
	for k, v := range m {
		switch vTyped := v.(type) {
		case map[string]interface{}:
			// 재귀적으로 비어있는 값 제거
			m[k] = RemoveEmptyValues(vTyped)
			// 비어있는 map이면 제거
			if len(m[k].(map[string]interface{})) == 0 {
				delete(m, k)
			}
		case []interface{}:
			// 슬라이스의 각 요소에 대해 재귀적으로 비어있는 값 제거
			for i, item := range vTyped {
				if itemMap, ok := item.(map[string]interface{}); ok {
					vTyped[i] = RemoveEmptyValues(itemMap)
				}
			}
			// 비어있는 슬라이스이면 제거
			if len(vTyped) == 0 {
				delete(m, k)
			}
		default:
			// nil 값이면 제거
			if v == nil {
				delete(m, k)
			}
		}
	}
	return m
}

func IsPathUnderDir(filePath string, dirPath string) bool {
	if strings.TrimSpace(dirPath) == "" {
		return false
	}

	fileClean := filepath.Clean(filepath.FromSlash(filePath))
	dirClean := filepath.Clean(filepath.FromSlash(dirPath))

	rel, err := filepath.Rel(dirClean, fileClean)
	if err != nil {
		return false
	}

	if rel == "." {
		return true
	}

	upPrefix := ".." + string(filepath.Separator)
	return rel != ".." && !strings.HasPrefix(rel, upPrefix)
}
