package messaging

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/Pocketwind/SWIFT-Launcher/auth"
	"github.com/Pocketwind/SWIFT-Launcher/config"
	"github.com/Pocketwind/SWIFT-Launcher/fsutil"
	"github.com/Pocketwind/SWIFT-Launcher/logging"
)

func MTSender(mdata MTData, token *auth.TokenData, settings *config.Settings, logCh chan<- logging.LogData, isPDE bool) (string, error) {
	token.RLock()
	privateKey := token.PrivateKey
	publicKey := token.PublicKey
	tokenType := token.TokenType
	accessToken := token.AccessToken
	token.RUnlock()

	//payload base64로 인코딩
	payloadB64 := base64.StdEncoding.EncodeToString([]byte(mdata.Payload))

	//json body 만들기
	mdata.Payload = payloadB64
	mdata.NetworkInfo.PossibleDuplicate = isPDE
	bodyBytes, err := json.Marshal(mdata)
	/*
		bodyMap := map[string]string{
			"sender_reference": mdata.SenderReference,
			"sender":           mdata.Sender,
			"receiver":         mdata.Receiver,
			"message_type":     mdata.MessageType,
			"payload":          payloadB64,
		}
		bodyBytes, err := json.Marshal(bodyMap)
	*/
	if err != nil {
		return "", fmt.Errorf("error marshaling request body: %w", err)
	}
	bodyString := string(bodyBytes)

	//NRSignature 만들기
	signature, err := auth.NRSignatureMaker(settings.Messaging.FinMessageUrl, settings.Messaging.Subject, bodyString, privateKey, publicKey)
	if err != nil {
		return "", fmt.Errorf("error creating NRSignature: %w", err)
	}

	//request 만들기
	finurl := settings.Messaging.FinMessageUrl
	req, err := http.NewRequest("POST", finurl, strings.NewReader(bodyString))
	if err != nil {
		return "", fmt.Errorf("error creating request: %w", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("%s %s", tokenType, accessToken))
	req.Header.Set("X-SWIFT-Signature", signature)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	//real request
	client := settings.Messaging.HttpClient
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("error making request: %w", err)
	}
	defer resp.Body.Close()
	response, _ := io.ReadAll(resp.Body)

	var messageResponse MessageResponse
	err = json.Unmarshal(response, &messageResponse)
	if err != nil {
		return "", fmt.Errorf("error parsing response: %w", err)
	}

	if messageResponse.MessageCloudReference == "" {
		return "", fmt.Errorf("message_cloud_reference not found in response")
	}

	return messageResponse.MessageCloudReference, nil
}

func MXSender(mdata MXData, token *auth.TokenData, settings *config.Settings, logCh chan<- logging.LogData, isPDE bool) (string, error) {
	token.RLock()
	privateKey := token.PrivateKey
	publicKey := token.PublicKey
	tokenType := token.TokenType
	accessToken := token.AccessToken
	token.RUnlock()

	//base64
	payloadB64 := base64.StdEncoding.EncodeToString([]byte(mdata.Payload))
	//json body
	/*
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
	*/
	mdata.Format = "MX"
	mdata.Payload = payloadB64
	mdata.NetworkInfo.PossibleDuplicate = isPDE
	bodyBytes, err := json.Marshal(mdata)
	if err != nil {
		return "", fmt.Errorf("error marshaling request body: %w", err)
	}
	bodyString := string(bodyBytes)

	//NRSignature 만들기
	signature, err := auth.NRSignatureMaker(settings.Messaging.InterActMessageUrl, settings.Messaging.Subject, bodyString, privateKey, publicKey)
	if err != nil {
		return "", fmt.Errorf("error creating NRSignature: %w", err)
	}

	//request 만들기
	interacturl := settings.Messaging.InterActMessageUrl
	req, err := http.NewRequest("POST", interacturl, strings.NewReader(bodyString))
	if err != nil {
		return "", fmt.Errorf("error creating request: %w", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("%s %s", tokenType, accessToken))
	req.Header.Set("X-SWIFT-Signature", signature)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	//real request
	client := settings.Messaging.HttpClient
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("error making request: %w", err)
	}
	defer resp.Body.Close()
	response, _ := io.ReadAll(resp.Body)

	var messageResponse MessageResponse
	err = json.Unmarshal(response, &messageResponse)
	if err != nil {
		return "", fmt.Errorf("error parsing response: %w", err)
	}

	if messageResponse.MessageCloudReference == "" {
		return "", fmt.Errorf("message_cloud_reference not found in response")
	}

	return messageResponse.MessageCloudReference, nil
}

func FileActSender(fadata FAData, filePath string, token *auth.TokenData, partner *config.Partner, settings *config.Settings, logCh chan<- logging.LogData, isPDE bool) (string, error) {

	//Initiate
	initiateResponse, err := fileActInitiate(fadata, token, settings, logCh, isPDE)
	if err != nil {
		return "", fmt.Errorf("error initiating file transfer: %w", err)
	}

	//Upload
	err = fileActUpload(fadata, initiateResponse, filePath, partner, settings, logCh)
	if err != nil {
		return "", fmt.Errorf("error uploading file: %w", err)
	}

	//Complete
	err = fileActComplete(initiateResponse.TransferID, token, settings, logCh)
	if err != nil {
		return "", fmt.Errorf("error completing file transfer: %w", err)
	}

	return initiateResponse.TransferID, nil
}
func fileActInitiate(fadata FAData, tokenData *auth.TokenData, settings *config.Settings, logCh chan<- logging.LogData, isPDE bool) (FileActInitiateResponse, error) {
	tokenData.RLock()
	privateKey := tokenData.PrivateKey
	publicKey := tokenData.PublicKey
	tokenType := tokenData.TokenType
	accessToken := tokenData.AccessToken
	tokenData.RUnlock()

	//PDE
	fadata.CompanionInfo.NetworkInfo.PossibleDuplicate = isPDE

	//header_info base64로 인코딩
	headerInfoBytes, err := json.Marshal(fadata.CompanionInfo.NetworkInfo.HeaderInfo)
	if err != nil {
		return FileActInitiateResponse{}, fmt.Errorf("error marshaling header info: %w", err)
	}
	fadata.CompanionInfo.NetworkInfo.HeaderInfo = base64.StdEncoding.EncodeToString(headerInfoBytes)

	//json body
	bodyBytes, err := json.Marshal(fadata)
	if err != nil {
		return FileActInitiateResponse{}, fmt.Errorf("error marshaling request body: %w", err)
	}
	bodyString := string(bodyBytes)

	//URL
	initiateURL := settings.Messaging.FileActUrl

	//NRSignature 만들기
	signature, err := auth.NRSignatureMaker(initiateURL, settings.Messaging.Subject, bodyString, privateKey, publicKey)
	if err != nil {
		return FileActInitiateResponse{}, fmt.Errorf("error creating NRSignature: %w", err)
	}

	//request 만들기
	req, err := http.NewRequest("POST", initiateURL, strings.NewReader(bodyString))
	if err != nil {
		return FileActInitiateResponse{}, fmt.Errorf("error creating request: %w", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("%s %s", tokenType, accessToken))
	req.Header.Set("X-SWIFT-Signature", signature)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	//real request
	client := settings.Messaging.HttpClient
	resp, err := client.Do(req)
	if err != nil {
		return FileActInitiateResponse{}, fmt.Errorf("error making request: %w", err)
	}
	defer resp.Body.Close()
	responseBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return FileActInitiateResponse{}, fmt.Errorf("error reading response body: %w", err)
	}
	var initiateResponse FileActInitiateResponse
	err = json.Unmarshal(responseBytes, &initiateResponse)
	if err != nil {
		return FileActInitiateResponse{}, fmt.Errorf("error parsing response: %w", err)
	}

	//에러 확인
	if resp.StatusCode != 201 {
		return initiateResponse, fmt.Errorf("file transfer initiation failed with status: %s\n%s", resp.Status, string(responseBytes))
	}

	return initiateResponse, nil
}
func fileActUpload(fadata FAData, initiateResponse FileActInitiateResponse, filePath string, partner *config.Partner, settings *config.Settings, logCh chan<- logging.LogData) error {
	var bodyPath string
	if partner != nil && partner.IsDFA {
		// DFA 파일은 collector에서 이미 input -> progress 이동이 끝난 경로를 사용한다.
		bodyPath = fsutil.PathHelper(filePath)
	} else {
		bodyPath = fsutil.PathHelper(partner.InputPath + "/" + fsutil.GetFileName(fadata.FileTransferRequest.FileAttributes.FileName))
	}

	body, err := os.Open(bodyPath)
	if err != nil {
		return fmt.Errorf("error opening file: %w", err)
	}
	defer body.Close()

	//real request
	client := settings.Messaging.HttpClient
	fileInfo, err := body.Stat()
	if err != nil {
		return fmt.Errorf("error getting file info: %w", err)
	}
	fileSize := fileInfo.Size()

	for _, signedURL := range initiateResponse.FileTransferResponse.SignedURLs {
		if signedURL.URL == "" {
			return fmt.Errorf("signed URL is empty for part %d", signedURL.Part)
		}
		// 파트마다 파일 처음으로 되감기
		if _, err := body.Seek(0, io.SeekStart); err != nil {
			return fmt.Errorf("error seeking file: %w", err)
		}
		req, err := http.NewRequest("PUT", signedURL.URL, body)
		if err != nil {
			return fmt.Errorf("error creating upload request: %w", err)
		}
		req.ContentLength = fileSize
		req.Header.Set("Content-MD5", fadata.FileTransferRequest.FileAttributes.FileDigest)
		req.Header.Set("Content-Length", strconv.Itoa(fadata.FileTransferRequest.FileAttributes.FileSize))
		req.Header.Set("x-amz-server-side-encryption-customer-algorithm", fadata.FileTransferRequest.EncryptionAttributes.KeyAlg)
		req.Header.Set("x-amz-server-side-encryption-customer-key", fadata.FileTransferRequest.EncryptionAttributes.KeyValue)
		req.Header.Set("x-amz-server-side-encryption-customer-key-md5", fadata.FileTransferRequest.EncryptionAttributes.KeyDigest)
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("error uploading file: %w", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("file upload failed with status: %s", resp.Status)
		}
	}

	return nil
}
func fileActComplete(transferID string, tokenData *auth.TokenData, settings *config.Settings, logCh chan<- logging.LogData) error {
	//Auth
	auth.Auth(settings, tokenData, logCh)

	tokenData.RLock()
	privateKey := tokenData.PrivateKey
	publicKey := tokenData.PublicKey
	tokenType := tokenData.TokenType
	accessToken := tokenData.AccessToken
	tokenData.RUnlock()

	//URL
	completeURL := settings.Messaging.FileActAckUrl
	completeURL = strings.ReplaceAll(completeURL, "{transfer-id}", transferID)

	//NRSignature 만들기
	signature, err := auth.NRSignatureMaker(completeURL, settings.Messaging.Subject, transferID, privateKey, publicKey)
	if err != nil {
		return fmt.Errorf("error creating NRSignature: %w", err)
	}
	//request 만들기
	req, err := http.NewRequest("POST", completeURL, nil)
	if err != nil {
		return fmt.Errorf("error creating complete request: %w", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("%s %s", tokenType, accessToken))
	req.Header.Set("X-SWIFT-Signature", signature)
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("Accept", "application/json")
	//query
	query := url.Values{}
	query.Set("transfer-id", transferID)
	req.URL.RawQuery = query.Encode()

	//real request
	client := settings.Messaging.HttpClient
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("error completing file transfer: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 201 {
		responseBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("file transfer completion failed with status: %s\n%s", resp.Status, string(responseBytes))
	}
	return nil
}
