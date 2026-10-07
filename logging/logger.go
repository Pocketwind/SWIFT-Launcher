package logging

import (
	"fmt"
	"io"
	"os"
	"time"
)

func Logger(exitCh <-chan bool, logCh <-chan LogData) {
	const maxLogSize = 10 * 1024 * 1024
	var logFile *os.File
	var logSize int64
	open := func() {
		var err error
		logFile, err = os.OpenFile("app.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cannot open app.log; logging to stderr: %v\n", err)
			return
		}
		if info, err := logFile.Stat(); err == nil {
			logSize = info.Size()
		}
	}
	open()
	defer func() {
		if logFile != nil {
			_ = logFile.Close()
		}
	}()
	write := func(data LogData) {
		if logFile != nil && logSize >= maxLogSize {
			_ = logFile.Close()
			logFile = nil
			if err := os.Rename("app.log", "app.log."+time.Now().Format("20060102150405.000000000")); err != nil {
				fmt.Fprintf(os.Stderr, "Cannot rotate app.log: %v\n", err)
			}
			logSize = 0
			open()
		}
		var destination io.Writer = os.Stderr
		if logFile != nil {
			destination = logFile
		}
		line := fmt.Sprintf("%s | %s | %s\n", timeFormat(data.Time), data.Type, data.Text)
		n, err := io.WriteString(destination, line)
		logSize += int64(n)
		if err != nil && logFile != nil {
			fmt.Fprintf(os.Stderr, "Cannot write app.log; logging to stderr: %v\n%s", err, line)
			_ = logFile.Close()
			logFile = nil
		}
	}

	write(LogData{
		Time: time.Now().UnixMilli(),
		Type: "INFO",
		Text: "Logger started",
	})

	//로그 루프
	// exitCh가 close되면 즉시 receive가 반환되므로,
	// logger가 먼저 종료되면 다른 goroutine의 Easylog가 block되어 graceful shutdown이 깨질 수 있다.
	// 따라서 exit 신호는 1회만 받고(exit=nil로 비활성화), 이후에도 logCh를 계속 drain 한다.
	exit := exitCh
	for {
		select {
		case <-exit:
			write(LogData{Time: time.Now().UnixMilli(), Type: "INFO", Text: "Shutdown requested"})
			exit = nil
		case logData, ok := <-logCh:
			if !ok {
				write(LogData{Time: time.Now().UnixMilli(), Type: "INFO", Text: "Logger stopped"})
				return
			}
			write(logData)
		}
	}
}

func Easylog(logCh chan<- LogData, logType string, logText string) {
	logData := LogData{
		Time: time.Now().UnixMilli(),
		Type: logType,
		Text: logText,
	}
	logCh <- logData
}

func timeFormat(millis int64) string {
	t := time.UnixMilli(millis)
	return t.Format("06-01-02 15:04:05.000")
}
