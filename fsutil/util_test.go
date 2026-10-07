package fsutil

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type failingReader struct{ started bool }

func (r *failingReader) Read(p []byte) (int, error) {
	if !r.started {
		r.started = true
		return copy(p, "partial"), nil
	}
	return 0, errors.New("connection lost")
}

func TestWaitFileReadyCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := WaitFileReadyContext(ctx, filepath.Join(t.TempDir(), "missing"), time.Minute); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}

func TestAtomicWriteFailurePreservesDestination(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "message.xml")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWriteReader(path, &failingReader{}, 0600); err == nil {
		t.Fatal("expected read failure")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "original" {
		t.Fatalf("destination changed: %q, %v", data, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("temporary file leaked: %v, %v", entries, err)
	}
	if err := AtomicWriteFile(path, []byte("complete"), 0600); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(path)
	if err != nil || string(data) != "complete" {
		t.Fatalf("publish failed: %q, %v", data, err)
	}
}

func TestWaitFileReadyWaitsThroughWriterPause(t *testing.T) {
	path := filepath.Join(t.TempDir(), "message.xml")
	if err := os.WriteFile(path, []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	updated := make(chan error, 1)
	go func() {
		time.Sleep(300 * time.Millisecond)
		updated <- os.WriteFile(path, []byte("second part"), 0600)
	}()
	start := time.Now()
	if err := WaitFileReady(path, 3*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := <-updated; err != nil {
		t.Fatal(err)
	}
	if time.Since(start) < 1200*time.Millisecond {
		t.Fatal("file considered ready during writer pause")
	}
}

func TestPathHelperPreservesRelativeParentAndUNC(t *testing.T) {
	if got := PathHelper("../inbox/message.xml"); got != "../inbox/message.xml" {
		t.Fatalf("parent path lost: %q", got)
	}
	if filepath.Separator == '\\' {
		if got := PathHelper(`\\server\share\folder\..\file.xml`); got != "//server/share/file.xml" {
			t.Fatalf("UNC path lost: %q", got)
		}
	}
	if got := PathHelper("  "); got != "" {
		t.Fatal(got)
	}
}
