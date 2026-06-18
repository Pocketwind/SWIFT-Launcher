package logging

import (
	"fmt"
	"os"
	"time"
)

func Logger(exitCh <-chan bool, logCh <-chan LogData) {
	//로그 로테이션 검사
	//로그 파일 생성
	logFile, err := os.OpenFile("app.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Printf("Error opening log file: %v\n", err)
		return
	}
	defer logFile.Close()

	//로그파일 크기 넘으면 로테이션
	const maxLogSize = 10 * 1024 * 1024 // 10MB
	info, err := logFile.Stat()
	if err == nil && info.Size() > maxLogSize {
		logFile.Close()
		os.Rename("app.log", fmt.Sprintf("app.log.%s", time.Now().Format("20060102150405")))
		logFile, err = os.OpenFile("app.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			fmt.Printf("Error opening log file: %v\n", err)
			return
		}
	}

	writer(logFile, LogData{
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
			writer(logFile, LogData{Time: time.Now().UnixMilli(), Type: "INFO", Text: "Shutdown requested"})
			exit = nil
		case logData, ok := <-logCh:
			if !ok {
				writer(logFile, LogData{Time: time.Now().UnixMilli(), Type: "INFO", Text: "Logger stopped"})
				return
			}
			writer(logFile, logData)
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

func writer(logFile *os.File, logData LogData) {
	timeString := timeFormat(logData.Time)
	logString := fmt.Sprintf("%s | %s | %s\n", timeString, logData.Type, logData.Text)
	logFile.WriteString(logString)
}

func timeFormat(millis int64) string {
	t := time.UnixMilli(millis)
	return t.Format("06-01-02 15:04:05.000")
}
