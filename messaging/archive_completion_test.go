package messaging

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Pocketwind/SWIFT-Launcher/config"
	"github.com/Pocketwind/SWIFT-Launcher/logging"
)

func TestCollectorHoldsAcceptedSourceWhenArchiveFails(t *testing.T) {
	setupValidationData(t)
	dir := t.TempDir()
	partner := &config.Partner{Name: "unit", ProgressPath: filepath.Join(dir, "progress"),
		ErrorPath: filepath.Join(dir, "error"), ArchiveEnabled: true, ArchivePath: filepath.Join(dir, "blocked")}
	if err := os.Mkdir(partner.ProgressPath, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(partner.ArchivePath, []byte("existing file"), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(partner.ProgressPath, "message.fin")
	if err := os.WriteFile(path, []byte(validValidationMT), 0600); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusCreated)
		io.WriteString(w, `{"message_cloud_reference":"accepted-ref"}`)
	}))
	defer server.Close()
	err := processMTFile(senderTestSettings(server), path, partner, senderTestToken(t), make(chan logging.LogData, 32), false)
	var uncertain *UncertainSendError
	if !errors.As(err, &uncertain) || calls.Load() != 1 {
		t.Fatalf("accepted archive failure was not held after one submission: calls=%d err=%v", calls.Load(), err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != validValidationMT {
		t.Fatalf("accepted source lost: %v", err)
	}
	if hold, err := os.ReadFile(path + ".hold.json"); err != nil || !strings.Contains(string(hold), "accepted-ref") {
		t.Fatalf("accepted reference missing from held state: %s, %v", hold, err)
	}
	if _, err := os.Stat(partner.ErrorPath); !os.IsNotExist(err) {
		t.Fatalf("accepted source routed as an ordinary failure: %v", err)
	}
}

func TestArchiveDisabledRemovesOnlyClaimedFile(t *testing.T) {
	dir := t.TempDir()
	partner := &config.Partner{ArchiveEnabled: false, ArchivePath: filepath.Join(dir, "archive")}
	path := filepath.Join(dir, "companion.xml")
	sharedPayload := filepath.Join(dir, "shared-body.dat")
	for _, name := range []string{path, sharedPayload} {
		if err := os.WriteFile(name, []byte("original content"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := completeOutgoing(path, partner, "accepted-ref", make(chan logging.LogData, 4), []byte("payload")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("claimed file still exists: %v", err)
	}
	if got, err := os.ReadFile(sharedPayload); err != nil || string(got) != "original content" {
		t.Fatalf("shared FileAct input payload was removed or changed: %v", err)
	}
	if _, err := os.Stat(partner.ArchivePath); !os.IsNotExist(err) {
		t.Fatalf("disabled archive was created: %v", err)
	}
}

func TestArchiveDisabledCleanupFailureIsHeld(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "message.fin")
	// A nonempty directory deterministically simulates a source that cannot be
	// removed on both Windows and Unix, without relying on user privileges.
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "retained"), []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	partner := &config.Partner{ArchiveEnabled: false, ArchivePath: filepath.Join(dir, "archive"), ErrorPath: filepath.Join(dir, "error")}
	err := completeOutgoing(path, partner, "accepted-ref", make(chan logging.LogData, 4))
	var uncertain *UncertainSendError
	if !errors.As(err, &uncertain) {
		t.Fatalf("accepted cleanup failure must be held: %v", err)
	}
	if _, err := os.Stat(filepath.Join(path, "retained")); err != nil {
		t.Fatalf("held source was removed: %v", err)
	}
	if hold, err := os.ReadFile(path + ".hold.json"); err != nil || !strings.Contains(string(hold), "accepted-ref") {
		t.Fatalf("held acceptance state missing: %s, %v", hold, err)
	}
}
