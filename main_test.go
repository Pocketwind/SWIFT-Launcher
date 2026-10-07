package main

import (
	"bufio"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Pocketwind/SWIFT-Launcher/auth"
	"github.com/Pocketwind/SWIFT-Launcher/config"
	"github.com/Pocketwind/SWIFT-Launcher/logging"
)

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
