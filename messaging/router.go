package messaging

import (
	"regexp"

	"github.com/Pocketwind/SWIFT-Launcher/config"
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
