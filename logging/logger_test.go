package logging

import (
	"os"
	"testing"
	"time"
)

func TestLoggerDrainsWhenLogFileCannotBeOpened(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.Mkdir("app.log", 0700); err != nil {
		t.Fatal(err)
	}
	logs := make(chan LogData)
	exit := make(chan bool)
	done := make(chan struct{})
	go func() { Logger(exit, logs); close(done) }()
	select {
	case logs <- LogData{Time: time.Now().UnixMilli(), Type: "INFO", Text: "worker finished"}:
	case <-time.After(2 * time.Second):
		t.Fatal("worker blocked after log open failure")
	}
	close(exit)
	close(logs)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("logger did not stop")
	}
}
