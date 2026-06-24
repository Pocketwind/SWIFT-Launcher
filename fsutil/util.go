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

func PathHelper(path string) string {
	if path == "" {
		return ""
	}

	cleaned := strings.ReplaceAll(path, "\\", "/")
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		return ""
	}

	if cleaned == "." {
		return "."
	}

	parts := strings.Split(cleaned, "/")
	stack := make([]string, 0, len(parts))

	for _, part := range parts {
		switch part {
		case "", ".":
			continue
		case "..":
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		default:
			stack = append(stack, part)
		}
	}

	if len(stack) == 0 {
		return "."
	}

	result := strings.Join(stack, "/")
	if strings.HasPrefix(path, "/") {
		return "/" + result
	}
	return result
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
