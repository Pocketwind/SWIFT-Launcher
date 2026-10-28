package messaging

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pocketwind/SWIFT-Launcher/config"
)

func TestErrorLogPreservesAllValidationDetails(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "message.fin")
	original := "original message body"
	if err := os.WriteFile(source, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	cause := errors.Join(errors.New("MT :32A: currency ZZZ is not registered"), errors.New("MT :59F: country ZZ is not registered"))
	partner := &config.Partner{ErrorPath: filepath.Join(dir, "error")}
	if err := routeSendFailure(source, partner, cause); !errors.Is(err, cause) {
		t.Fatalf("original error was not returned: %v", err)
	}
	logs, err := filepath.Glob(filepath.Join(partner.ErrorPath, "failed-*", "message.fin.log"))
	if err != nil || len(logs) != 1 {
		t.Fatalf("per-message error log missing: %v, %v", logs, err)
	}
	if got, err := os.ReadFile(logs[0]); err != nil || string(got) != cause.Error()+"\n" {
		t.Fatalf("validation details were changed or missing: %q, %v", got, err)
	}
	if got, err := os.ReadFile(strings.TrimSuffix(logs[0], ".log")); err != nil || string(got) != original {
		t.Fatalf("original message was changed or lost: %q, %v", got, err)
	}
}

func TestErrorLogWriteFailureRetainsOriginal(t *testing.T) {
	dir := t.TempDir()
	// The original fits a 255-byte filename limit; its additional .log suffix
	// does not. This forces log creation to fail without privilege assumptions.
	source := filepath.Join(dir, strings.Repeat("m", 249)+".fin")
	original := []byte("failed message must remain available")
	if err := os.WriteFile(source, original, 0600); err != nil {
		t.Fatal(err)
	}
	cause := errors.New("currency validation failed")
	partner := &config.Partner{ErrorPath: filepath.Join(dir, "error")}
	err := routeSendFailure(source, partner, cause)
	if !errors.Is(err, cause) || !strings.Contains(err.Error(), "failed to save message error log") {
		t.Fatalf("expected log write failure with original cause, got %v", err)
	}
	if got, err := os.ReadFile(source); err != nil || string(got) != string(original) {
		t.Fatalf("log failure removed or changed the source: %q, %v", got, err)
	}
}
