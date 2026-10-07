package messaging

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/Pocketwind/SWIFT-Launcher/config"
	"github.com/Pocketwind/SWIFT-Launcher/fsutil"
)

func MXRouter(route config.Route, message MXMessage) bool {
	var checkList []bool

	re, err := regexp.Compile(route.Sender)
	if err != nil {
		return false
	}
	checkList = append(checkList, re.MatchString(message.Requestor))

	re, err = regexp.Compile(route.Receiver)
	if err != nil {
		return false
	}
	checkList = append(checkList, re.MatchString(message.Responder))

	re, err = regexp.Compile(route.MessageType)
	if err != nil {
		return false
	}
	checkList = append(checkList, re.MatchString(message.MessageType))

	for _, check := range checkList {
		if !check {
			return false
		}
	}
	return true
}

func MTRouter(route config.Route, message MTMessage) bool {
	var checkList []bool

	re, err := regexp.Compile(route.Sender)
	if err != nil {
		return false
	}
	checkList = append(checkList, re.MatchString(message.Sender))

	re, err = regexp.Compile(route.Receiver)
	if err != nil {
		return false
	}
	checkList = append(checkList, re.MatchString(message.Receiver))

	re, err = regexp.Compile(route.MessageType)
	if err != nil {
		return false
	}
	checkList = append(checkList, re.MatchString(message.MessageType))

	for _, check := range checkList {
		if !check {
			return false
		}
	}
	return true
}

func ErrorMessageRouter(filePath string, partner *config.Partner) error {
	if partner.ErrorPath == "" {
		return fmt.Errorf("error directory is not configured; original retained in progress")
	}
	if err := os.MkdirAll(partner.ErrorPath, 0700); err != nil {
		return fmt.Errorf("creating error directory: %w", err)
	}
	// Use a unique attempt directory so an earlier failed original survives
	// when another input arrives with the same filename.
	dir, err := os.MkdirTemp(partner.ErrorPath, "failed-*")
	if err != nil {
		return fmt.Errorf("creating error archive: %w", err)
	}
	err = os.Rename(fsutil.PathHelper(filePath), filepath.Join(dir, fsutil.GetFileName(filePath)))
	if err != nil {
		return fmt.Errorf("failed to move file to error directory: %w", err)
	}
	return nil
}

func FileActRouter(partner config.Partner, companionInfo CompanionInfo) bool {
	var checkList []bool

	re, err := regexp.Compile(partner.Route.Sender)
	if err != nil {
		return false
	}
	checkList = append(checkList, re.MatchString(companionInfo.Requestor))

	re, err = regexp.Compile(partner.Route.Receiver)
	if err != nil {
		return false
	}
	checkList = append(checkList, re.MatchString(companionInfo.Responder))

	re, err = regexp.Compile(partner.Route.MessageType)
	if err != nil {
		return false
	}
	checkList = append(checkList, re.MatchString(companionInfo.MessageType))

	re, err = regexp.Compile(partner.DFAInfo.ServiceCode)
	if err != nil {
		return false
	}
	checkList = append(checkList, re.MatchString(companionInfo.ServiceCode))

	for _, check := range checkList {
		if !check {
			return false
		}
	}
	return true
}
