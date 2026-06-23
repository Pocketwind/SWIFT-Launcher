package messaging

import (
	"fmt"
	"os"
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
	err := os.Rename(fsutil.PathHelper(filePath), fsutil.PathHelper(partner.ErrorPath+"/"+fsutil.GetFileName(filePath)))
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
