package messaging

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Pocketwind/SWIFT-Launcher/config"
)

func TestArchiveSentFileUsesTimeDirectoriesAndPreservesOriginalNames(t *testing.T) {
	root := t.TempDir()
	partner := &config.Partner{Name: "unit", ProgressPath: filepath.Join(root, "progress"), ArchivePath: filepath.Join(root, "archive")}
	if err := os.MkdirAll(partner.ProgressPath, 0700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(partner.ProgressPath, "payment.fin")
	versions := [][]byte{[]byte("FIRST\r\n\x00original bytes"), []byte("SECOND\noriginal bytes")}
	var previous string
	for index, original := range versions {
		if err := os.WriteFile(source, original, 0600); err != nil {
			t.Fatal(err)
		}
		archived, err := archiveSentFile(source, partner, "reference")
		if err != nil {
			t.Fatal(err)
		}
		archiveDir := filepath.Dir(archived)
		if filepath.Dir(archiveDir) != partner.ArchivePath || filepath.Base(archived) != "payment.fin" {
			t.Fatalf("unexpected archive filename: %s", archived)
		}
		stamp, _, _ := strings.Cut(filepath.Base(archiveDir), "-")
		if _, err := time.Parse("20060102T150405.000000000Z", stamp); err != nil {
			t.Fatalf("archive directory does not start with a UTC timestamp: %s, %v", archiveDir, err)
		}
		if archived == previous {
			t.Fatal("same-name versions reused an archive filename")
		}
		if _, err := os.Stat(source); !os.IsNotExist(err) {
			t.Fatalf("progress original remains after successful archive: %v", err)
		}
		contents, err := os.ReadFile(archived)
		if err != nil || !bytes.Equal(contents, original) {
			t.Fatalf("archived original bytes changed: %q, %v", contents, err)
		}
		if index > 0 {
			contents, err := os.ReadFile(previous)
			if err != nil || !bytes.Equal(contents, versions[index-1]) {
				t.Fatalf("previous archive overwritten: %q, %v", contents, err)
			}
		}
		previous = archived
	}
	entries, err := os.ReadDir(partner.ArchivePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected one time directory per submission, got %d entries", len(entries))
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			t.Fatalf("archive wrote a file outside its time directory: %s", entry.Name())
		}
		files, err := os.ReadDir(filepath.Join(partner.ArchivePath, entry.Name()))
		if err != nil || len(files) != 2 {
			t.Fatalf("expected original and receipt in time directory: %v, %v", files, err)
		}
		for _, file := range files {
			if file.IsDir() || file.Name() != "payment.fin" && file.Name() != "receipt.json" {
				t.Fatalf("unexpected archived entry: %s", file.Name())
			}
		}
	}
}

func TestArchiveSentFileAssociatesFileActPayloadAndReceipt(t *testing.T) {
	root := t.TempDir()
	partner := &config.Partner{Name: "fileact-unit", ProgressPath: root, ArchivePath: filepath.Join(root, "archive")}
	source := filepath.Join(root, "companion.xml")
	original := []byte("<Body>shared-data.bin</Body>\r\n")
	payload := []byte{0, 1, 2, 0xff, '\r', '\n'}
	if err := os.WriteFile(source, original, 0600); err != nil {
		t.Fatal(err)
	}
	sharedPayload := filepath.Join(root, "shared-data.bin")
	if err := os.WriteFile(sharedPayload, payload, 0600); err != nil {
		t.Fatal(err)
	}
	archived, err := archiveSentFile(source, partner, "fileact-reference", payload)
	if err != nil {
		t.Fatal(err)
	}
	archiveDir := filepath.Dir(archived)
	for path, expected := range map[string][]byte{archived: original, filepath.Join(archiveDir, "payload.bin"): payload, sharedPayload: payload} {
		contents, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(contents, expected) {
			t.Fatalf("artifact %s changed or missing: %q, %v", path, contents, err)
		}
	}
	contents, err := os.ReadFile(filepath.Join(archiveDir, "receipt.json"))
	if err != nil {
		t.Fatal(err)
	}
	var receipt map[string]string
	if err := json.Unmarshal(contents, &receipt); err != nil {
		t.Fatal(err)
	}
	for key, expected := range map[string]string{"state": "submitted", "reference": "fileact-reference", "partner": partner.Name, "file": "companion.xml", "archive_file": filepath.Base(archived)} {
		if receipt[key] != expected {
			t.Errorf("receipt %s = %q, want %q", key, receipt[key], expected)
		}
	}
	if _, err := time.Parse(time.RFC3339Nano, receipt["time"]); err != nil {
		t.Fatalf("receipt lacks valid acceptance time: %v", err)
	}
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Fatalf("companion remains after successful archive: %v", err)
	}
}

func TestArchiveSentFileRetainsSourceWhenArchiveDirectoryFails(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "message.fin")
	original := []byte("unchanged original\r\n")
	blocked := filepath.Join(root, "blocked")
	for path, data := range map[string][]byte{source: original, blocked: []byte("existing file")} {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	partner := &config.Partner{ArchivePath: filepath.Join(blocked, "archive")}
	if archived, err := archiveSentFile(source, partner, "accepted-reference"); err == nil || archived != "" {
		t.Fatalf("failed archive returned success: %q, %v", archived, err)
	}
	contents, err := os.ReadFile(source)
	if err != nil || !bytes.Equal(contents, original) {
		t.Fatalf("failed archive changed or removed original: %q, %v", contents, err)
	}
	contents, err = os.ReadFile(blocked)
	if err != nil || string(contents) != "existing file" {
		t.Fatalf("existing destination file changed: %q, %v", contents, err)
	}
}

func TestArchiveSentFileRejectsReservedReceiptFilename(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "receipt.json")
	original := []byte("accepted original must remain\r\n")
	if err := os.WriteFile(source, original, 0600); err != nil {
		t.Fatal(err)
	}
	partner := &config.Partner{ArchivePath: filepath.Join(root, "archive")}
	archived, err := archiveSentFile(source, partner, "accepted-reference")
	if err == nil || archived != "" || !strings.Contains(err.Error(), "reserved archive filename") {
		t.Fatalf("expected reserved filename failure, got %q, %v", archived, err)
	}
	contents, err := os.ReadFile(source)
	if err != nil || !bytes.Equal(contents, original) {
		t.Fatalf("reserved filename rejection changed or removed original: %q, %v", contents, err)
	}
	entries, err := os.ReadDir(partner.ArchivePath)
	if err != nil && !os.IsNotExist(err) || len(entries) != 0 {
		t.Fatalf("reserved filename created incomplete archive: %v, %v", entries, err)
	}
}

func TestArchiveSentFileReservesPayloadFilenameOnlyWithSeparatePayload(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "payload.bin")
	original := []byte("original payload.bin input")
	partner := &config.Partner{ArchivePath: filepath.Join(root, "archive")}
	if err := os.WriteFile(source, original, 0600); err != nil {
		t.Fatal(err)
	}
	archived, err := archiveSentFile(source, partner, "first-reference")
	if err != nil {
		t.Fatalf("payload.bin without a separate FileAct payload rejected: %v", err)
	}
	if filepath.Base(archived) != "payload.bin" {
		t.Fatalf("original payload.bin filename changed: %s", archived)
	}
	if err := os.WriteFile(source, original, 0600); err != nil {
		t.Fatal(err)
	}
	if path, err := archiveSentFile(source, partner, "second-reference", []byte("separate FileAct body")); err == nil || path != "" || !strings.Contains(err.Error(), "reserved archive filename") {
		t.Fatalf("conflicting payload.bin source accepted: %q, %v", path, err)
	}
	for _, path := range []string{source, archived} {
		contents, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(contents, original) {
			t.Fatalf("reserved filename rejection changed existing file %s: %q, %v", path, contents, err)
		}
	}
	entries, err := os.ReadDir(partner.ArchivePath)
	if err != nil || len(entries) != 1 {
		t.Fatalf("reserved filename rejection changed existing archive directories: %v, %v", entries, err)
	}
}

func TestCreateArchiveDirectoryPreservesSameTimeArchives(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 10, 27, 12, 34, 56, 123456789, time.FixedZone("KST", 9*60*60))
	const timestamp = "20261027T033456.123456789Z"
	first, err := createArchiveDirectory(root, now)
	if err != nil || filepath.Base(first) != timestamp {
		t.Fatalf("unexpected initial UTC time directory: %s, %v", first, err)
	}
	evidence := filepath.Join(first, "receipt.json")
	if err := os.WriteFile(evidence, []byte("existing acceptance evidence"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"-1", "-2"} {
		dir, err := createArchiveDirectory(root, now)
		if err != nil || filepath.Base(dir) != timestamp+suffix {
			t.Fatalf("same-time collision did not get a unique directory: %s, %v", dir, err)
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 0 {
			t.Fatalf("new archive directory contains previous archive files: %v, %v", entries, err)
		}
	}
	contents, err := os.ReadFile(evidence)
	if err != nil || string(contents) != "existing acceptance evidence" {
		t.Fatalf("same-time collision changed existing archive: %q, %v", contents, err)
	}
}
