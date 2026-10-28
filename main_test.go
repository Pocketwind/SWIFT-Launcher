package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Pocketwind/SWIFT-Launcher/auth"
	"github.com/Pocketwind/SWIFT-Launcher/config"
	"github.com/Pocketwind/SWIFT-Launcher/logging"
)

func TestPrepareArchiveDirectoriesHonorsEnabledOption(t *testing.T) {
	dir := t.TempDir()
	partners := []config.Partner{
		{Name: "enabled", Status: true, ArchiveEnabled: true, InputPath: filepath.Join(dir, "input"), ProgressPath: filepath.Join(dir, "progress")},
		{Name: "disabled", Status: true, ArchiveEnabled: false, ArchivePath: filepath.Join(dir, "disabled")},
		{Name: "inactive", Status: false, ArchiveEnabled: true, ArchivePath: filepath.Join(dir, "inactive")},
	}
	if err := prepareArchiveDirectories(partners); err != nil {
		t.Fatal(err)
	}
	for i, partner := range partners {
		_, err := os.Stat(partner.ArchiveDirectory())
		if i == 0 && err != nil || i != 0 && !os.IsNotExist(err) {
			t.Fatalf("unexpected archive directory state for %s: %v", partner.Name, err)
		}
	}
}

func TestPrepareArchiveDirectoriesRejectsWatchedInput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input")
	for _, destination := range []string{input, filepath.Join(input, "nested")} {
		partners := []config.Partner{
			{Name: "archive owner", Status: true, ArchiveEnabled: true, ArchivePath: destination},
			{Name: "other input", Status: true, InputPath: input},
		}
		if err := prepareArchiveDirectories(partners); err == nil || !strings.Contains(err.Error(), "other input") {
			t.Fatalf("archive inside a watched input directory accepted: %v", err)
		}
		if _, err := os.Stat(input); !os.IsNotExist(err) {
			t.Fatalf("invalid archive directory was created: %v", err)
		}
	}
}

func TestConsoleReaderPreservesInteractiveCommandInput(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("refresh\nreference\nauthcode\nexit\n"))
	exit := make(chan bool)
	requests, inputs, done := startConsoleReader(reader, exit)
	defer close(exit)
	requests <- struct{}{}
	if got := <-inputs; got != "refresh\n" {
		t.Fatal(got)
	}
	// A command can consume its arguments without the background reader racing it.
	for _, want := range []string{"reference\n", "authcode\n"} {
		got, err := reader.ReadString('\n')
		if err != nil || got != want {
			t.Fatalf("command input lost: got %q, want %q, err %v", got, want, err)
		}
	}
	requests <- struct{}{}
	if got := <-inputs; got != "exit\n" {
		t.Fatal(got)
	}
	requests <- struct{}{}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("EOF did not stop console reader")
	}
}

func TestShutdownWaitsForWorkerAndDrainsLogger(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)
	var once sync.Once
	logs := make(chan logging.LogData, 4)
	loggerDone := make(chan struct{})
	drained := make(chan bool, 1)
	go func() {
		seen := false
		for log := range logs {
			if log.Text == "worker persisted" {
				seen = true
			}
		}
		drained <- seen
		close(loggerDone)
	}()
	stopRequested := make(chan struct{})
	shutdown := createShutdown(func(string) { close(stopRequested) }, &wg, &once, &auth.TokenData{}, &config.Settings{}, logs, loggerDone)
	finished := make(chan struct{})
	go func() { shutdown("test"); close(finished) }()
	<-stopRequested
	select {
	case <-finished:
		t.Fatal("shutdown returned before worker persistence")
	default:
	}
	logging.Easylog(logs, "INFO", "worker persisted")
	wg.Done()
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not finish")
	}
	if !<-drained {
		t.Fatal("worker log not drained")
	}
	shutdown("repeat") // Must not close channels twice.
}
