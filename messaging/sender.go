package messaging

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Pocketwind/SWIFT-Launcher/auth"
	"github.com/Pocketwind/SWIFT-Launcher/config"
	"github.com/Pocketwind/SWIFT-Launcher/logging"
)

func MTSender(mdata MTData, token *auth.TokenData, settings *config.Settings, logCh chan<- logging.LogData) (string, error) {
	token.RLock()
	privateKey := token.PrivateKey
	publicKey := token.PublicKey
	tokenType := token.TokenType
	accessToken := token.AccessToken
	token.RUnlock()

	//payload base64로 인코딩
	payloadB64 := base64.StdEncoding.EncodeToString([]byte(mdata.Payload))

	//json body 만들기
	bodyMap := map[string]string{
		"sender_reference": mdata.SenderReference,
		"sender":           mdata.Sender,
		"receiver":         mdata.Receiver,
		"message_type":     mdata.MessageType,
		"payload":          payloadB64,
	}
	bodyBytes, err := json.Marshal(bodyMap)
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error marshaling request body: %v", err))
		return "Error marshaling request body", err
	}
	bodyString := string(bodyBytes)

	//NRSignature 만들기
	signature, err := auth.NRSignatureMaker(settings.Messaging.FinMessageUrl, settings.Messaging.Subject, bodyString, privateKey, publicKey)
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error creating NRSignature: %v", err))
		return "Error creating NRSignature", err
	}

	//request 만들기
	finurl := settings.Messaging.FinMessageUrl
	req, err := http.NewRequest("POST", finurl, strings.NewReader(bodyString))
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error creating request: %v", err))
		return "Error creating request", err
	}
	req.Header.Set("Authorization", fmt.Sprintf("%s %s", tokenType, accessToken))
	req.Header.Set("X-SWIFT-Signature", signature)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	//real request
	client := settings.Messaging.HttpClient
	resp, err := client.Do(req)
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error making request: %v", err))
		return "Error making request", err
	}
	defer resp.Body.Close()
	response, _ := io.ReadAll(resp.Body)

	var messageResponse MessageResponse
	err = json.Unmarshal(response, &messageResponse)
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error parsing response: %v", err))
		return "Error parsing response", err
	}

	if messageResponse.MessageCloudReference == "" {
		logging.Easylog(logCh, "ERROR", "message_cloud_reference not found in response")
		return "message_cloud_reference not found in response", fmt.Errorf("message_cloud_reference not found in response")
	}

	return messageResponse.MessageCloudReference, nil
}

func MXSender(mdata MXData, token *auth.TokenData, settings *config.Settings, logCh chan<- logging.LogData) (string, error) {
	token.RLock()
	privateKey := token.PrivateKey
	publicKey := token.PublicKey
	tokenType := token.TokenType
	accessToken := token.AccessToken
	token.RUnlock()

	//base64
	payloadB64 := base64.StdEncoding.EncodeToString([]byte(mdata.Payload))
	//json body
	bodyMap := map[string]string{
		"service_code":     mdata.ServiceCode,
		"payload":          payloadB64,
		"sender_reference": mdata.SenderReference,
		"message_type":     mdata.MessageType,
		"requestor":        mdata.Requestor,
		"responder":        mdata.Responder,
		"format":           "MX",
	}
	bodyBytes, err := json.Marshal(bodyMap)
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error marshaling request body: %v", err))
		return "Error marshaling request body", err
	}
	bodyString := string(bodyBytes)

	//NRSignature 만들기
	signature, err := auth.NRSignatureMaker(settings.Messaging.InterActMessageUrl, settings.Messaging.Subject, bodyString, privateKey, publicKey)
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error creating NRSignature: %v", err))
		return "Error creating NRSignature", err
	}

	//request 만들기
	interacturl := settings.Messaging.InterActMessageUrl
	req, err := http.NewRequest("POST", interacturl, strings.NewReader(bodyString))
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error creating request: %v", err))
		return "Error creating request", err
	}
	req.Header.Set("Authorization", fmt.Sprintf("%s %s", tokenType, accessToken))
	req.Header.Set("X-SWIFT-Signature", signature)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	//real request
	client := settings.Messaging.HttpClient
	resp, err := client.Do(req)
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error making request: %v", err))
		return "Error making request", err
	}
	defer resp.Body.Close()
	response, _ := io.ReadAll(resp.Body)

	var messageResponse MessageResponse
	err = json.Unmarshal(response, &messageResponse)
	if err != nil {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error parsing response: %v", err))
		return "Error parsing response", err
	}

	if messageResponse.MessageCloudReference == "" {
		logging.Easylog(logCh, "ERROR", "message_cloud_reference not found in response")
		return "message_cloud_reference not found in response", fmt.Errorf("message_cloud_reference not found in response")
	}

	return messageResponse.MessageCloudReference, nil
}
