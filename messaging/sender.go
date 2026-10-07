package messaging

import (
	"bytes"
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/Pocketwind/SWIFT-Launcher/auth"
	"github.com/Pocketwind/SWIFT-Launcher/config"
	"github.com/Pocketwind/SWIFT-Launcher/fsutil"
	"github.com/Pocketwind/SWIFT-Launcher/logging"
	"github.com/antchfx/xmlquery"
)

// UncertainSendError means an operation may have been accepted remotely. Hold
// its original for reconciliation rather than automatically submitting again.
type UncertainSendError struct {
	Operation string
	Err       error
}

func (e *UncertainSendError) Error() string {
	return fmt.Sprintf("%s: result is uncertain; verify remote status before resubmitting: %v", e.Operation, e.Err)
}

func (e *UncertainSendError) Unwrap() error { return e.Err }

func uncertainSend(operation string, err error) error {
	return &UncertainSendError{Operation: operation, Err: err}
}

const maxSendResponseSize = 1024 * 1024

// Redirects must not resubmit signed POSTs or expose credentials elsewhere.
func sendHTTP(client *http.Client, req *http.Request) (*http.Response, error) {
	if client == nil {
		return nil, fmt.Errorf("HTTP client is not configured")
	}
	copyClient := *client
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return copyClient.Do(req)
}

func sendStatusError(operation string, status int) error {
	err := fmt.Errorf("HTTP status %d", status)
	// Timeouts, conflicts and throttling can follow accepted submissions.
	if status >= 400 && status < 500 && status != 408 && status != 409 && status != 425 && status != 429 {
		return fmt.Errorf("%s rejected: %w", operation, err)
	}
	return uncertainSend(operation, err)
}

func readSendResponse(resp *http.Response) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxSendResponseSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxSendResponseSize {
		return nil, fmt.Errorf("response body exceeds %d bytes", maxSendResponseSize)
	}
	return data, nil
}

func signedRequest(method, rawURL, body string, token *auth.TokenData, settings *config.Settings) (*http.Request, error) {
	token.RLock()
	privateKey, publicKey := token.PrivateKey, token.PublicKey
	tokenType, accessToken := token.TokenType, token.AccessToken
	subject := token.Subject
	token.RUnlock()
	if strings.TrimSpace(accessToken) == "" || strings.TrimSpace(tokenType) == "" {
		return nil, fmt.Errorf("access token is unavailable")
	}
	signature, err := auth.NRSignatureMaker(rawURL, subject, body, privateKey, publicKey)
	if err != nil {
		return nil, fmt.Errorf("creating NRSignature: %w", err)
	}
	req, err := http.NewRequest(method, rawURL, strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("%s %s", tokenType, accessToken))
	req.Header.Set("X-SWIFT-Signature", signature)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	return req, nil
}

func sendMessage(data any, rawURL string, token *auth.TokenData, settings *config.Settings, logCh chan<- logging.LogData) (string, error) {
	if err := auth.Auth(settings, token, logCh); err != nil {
		return "", fmt.Errorf("authentication failed: %w", err)
	}
	body, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("marshaling request body: %w", err)
	}
	req, err := signedRequest(http.MethodPost, rawURL, string(body), token, settings)
	if err != nil {
		return "", err
	}
	resp, err := sendHTTP(settings.Messaging.HttpClient, req)
	if err != nil {
		// Transport errors may include sensitive query parameters.
		return "", uncertainSend("message submission", errors.New("request did not complete"))
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", sendStatusError("message submission", resp.StatusCode)
	}
	response, err := readSendResponse(resp)
	if err != nil {
		return "", uncertainSend("message submission", fmt.Errorf("reading response: %w", err))
	}
	var result MessageResponse
	if err := json.Unmarshal(response, &result); err != nil {
		return "", uncertainSend("message submission", fmt.Errorf("parsing response: %w", err))
	}
	if strings.TrimSpace(result.MessageCloudReference) == "" {
		return "", uncertainSend("message submission", errors.New("message_cloud_reference missing from response"))
	}
	return result.MessageCloudReference, nil
}

func MTSender(mdata MTData, token *auth.TokenData, settings *config.Settings, logCh chan<- logging.LogData, isPDE bool) (string, error) {
	mdata.Payload = base64.StdEncoding.EncodeToString([]byte(mdata.Payload))
	mdata.NetworkInfo.PossibleDuplicate = isPDE
	return sendMessage(mdata, settings.Messaging.FinMessageUrl, token, settings, logCh)
}

func MXSender(mdata MXData, token *auth.TokenData, settings *config.Settings, logCh chan<- logging.LogData, isPDE bool) (string, error) {
	mdata.Payload = base64.StdEncoding.EncodeToString([]byte(mdata.Payload))
	mdata.Format = "MX"
	mdata.NetworkInfo.PossibleDuplicate = isPDE
	return sendMessage(mdata, settings.Messaging.InterActMessageUrl, token, settings, logCh)
}

func FileActSender(fadata FAData, filePath string, token *auth.TokenData, partner *config.Partner, settings *config.Settings, logCh chan<- logging.LogData, isPDE bool) (string, error) {
	// Validate an immutable snapshot before creating a remote transfer.
	payload, err := loadFileActPayload(fadata, filePath, partner)
	if err != nil {
		return "", err
	}
	initiate, err := fileActInitiate(fadata, token, settings, logCh, isPDE)
	if err != nil {
		return "", fmt.Errorf("initiating file transfer: %w", err)
	}
	if err := fileActUploadPayload(fadata, initiate, payload, settings); err != nil {
		return "", uncertainSend("file transfer "+initiate.TransferID, err)
	}
	if err := fileActComplete(initiate.TransferID, token, settings, logCh); err != nil {
		return "", uncertainSend("file transfer "+initiate.TransferID, err)
	}
	return initiate.TransferID, nil
}

func fileActInitiate(fadata FAData, token *auth.TokenData, settings *config.Settings, logCh chan<- logging.LogData, isPDE bool) (FileActInitiateResponse, error) {
	if err := auth.Auth(settings, token, logCh); err != nil {
		return FileActInitiateResponse{}, fmt.Errorf("authentication failed: %w", err)
	}
	fadata.CompanionInfo.NetworkInfo.PossibleDuplicate = isPDE
	headerInfo, err := json.Marshal(fadata.CompanionInfo.NetworkInfo.HeaderInfo)
	if err != nil {
		return FileActInitiateResponse{}, fmt.Errorf("marshaling header info: %w", err)
	}
	fadata.CompanionInfo.NetworkInfo.HeaderInfo = base64.StdEncoding.EncodeToString(headerInfo)
	body, err := json.Marshal(fadata)
	if err != nil {
		return FileActInitiateResponse{}, fmt.Errorf("marshaling request body: %w", err)
	}
	req, err := signedRequest(http.MethodPost, settings.Messaging.FileActUrl, string(body), token, settings)
	if err != nil {
		return FileActInitiateResponse{}, err
	}
	resp, err := sendHTTP(settings.Messaging.HttpClient, req)
	if err != nil {
		return FileActInitiateResponse{}, uncertainSend("file transfer initiation", errors.New("request did not complete"))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return FileActInitiateResponse{}, sendStatusError("file transfer initiation", resp.StatusCode)
	}
	response, err := readSendResponse(resp)
	if err != nil {
		return FileActInitiateResponse{}, uncertainSend("file transfer initiation", fmt.Errorf("reading response: %w", err))
	}
	var result FileActInitiateResponse
	if err := json.Unmarshal(response, &result); err != nil {
		return result, uncertainSend("file transfer initiation", fmt.Errorf("parsing response: %w", err))
	}
	if strings.TrimSpace(result.TransferID) == "" {
		return result, uncertainSend("file transfer initiation", errors.New("transfer_id missing from response"))
	}
	return result, nil
}

func loadFileActPayload(fadata FAData, filePath string, partner *config.Partner) ([]byte, error) {
	if partner == nil {
		return nil, fmt.Errorf("FileAct partner is not configured")
	}
	bodyPath := fsutil.PathHelper(filePath)
	if !partner.IsDFA {
		// FileLogicalName is the remote label. Body names the local file.
		companion, err := os.Open(bodyPath)
		if err != nil {
			return nil, fmt.Errorf("opening FileAct companion: %w", err)
		}
		doc, err := xmlquery.Parse(companion)
		companion.Close()
		if err != nil {
			return nil, fmt.Errorf("parsing FileAct companion: %w", err)
		}
		bodyName := strings.TrimSpace(fsutil.SafeInnerText(xmlquery.FindOne(doc, "//*[local-name()='Body']")))
		bodyName = filepath.Base(filepath.FromSlash(strings.ReplaceAll(bodyName, "\\", "/")))
		if bodyName == "" || bodyName == "." || bodyName == ".." {
			return nil, fmt.Errorf("FileAct companion Body filename is missing")
		}
		bodyPath = filepath.Join(partner.InputPath, bodyName)
	}
	payload, err := os.ReadFile(bodyPath)
	if err != nil {
		return nil, fmt.Errorf("reading upload file: %w", err)
	}
	if len(payload) != fadata.FileTransferRequest.FileAttributes.FileSize {
		return nil, fmt.Errorf("upload file size changed after metadata creation")
	}
	digest := md5.Sum(payload)
	if base64.StdEncoding.EncodeToString(digest[:]) != fadata.FileTransferRequest.FileAttributes.FileDigest {
		return nil, fmt.Errorf("upload file digest differs from transfer metadata")
	}
	return payload, nil
}

func fileActUpload(fadata FAData, initiate FileActInitiateResponse, filePath string, partner *config.Partner, settings *config.Settings, logCh chan<- logging.LogData) error {
	payload, err := loadFileActPayload(fadata, filePath, partner)
	if err != nil {
		return err
	}
	return fileActUploadPayload(fadata, initiate, payload, settings)
}

func fileActUploadPayload(fadata FAData, initiate FileActInitiateResponse, payload []byte, settings *config.Settings) error {
	// No part-size/offset metadata is available. Repeating the complete file
	// at each URL corrupts multipart transfers, so reject them explicitly.
	if len(initiate.FileTransferResponse.SignedURLs) != 1 {
		return fmt.Errorf("expected one upload URL; multipart upload is unsupported")
	}
	signedURL := initiate.FileTransferResponse.SignedURLs[0]
	if strings.TrimSpace(signedURL.URL) == "" {
		return fmt.Errorf("signed upload URL is empty")
	}
	req, err := http.NewRequest(http.MethodPut, signedURL.URL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("invalid signed upload URL")
	}
	req.Header.Set("Content-MD5", fadata.FileTransferRequest.FileAttributes.FileDigest)
	req.Header.Set("x-amz-server-side-encryption-customer-algorithm", fadata.FileTransferRequest.EncryptionAttributes.KeyAlg)
	req.Header.Set("x-amz-server-side-encryption-customer-key", fadata.FileTransferRequest.EncryptionAttributes.KeyValue)
	req.Header.Set("x-amz-server-side-encryption-customer-key-md5", fadata.FileTransferRequest.EncryptionAttributes.KeyDigest)
	resp, err := sendHTTP(settings.Messaging.HttpClient, req)
	if err != nil {
		return fmt.Errorf("upload request did not complete")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("file upload returned HTTP status %d", resp.StatusCode)
	}
	return nil
}

func fileActComplete(transferID string, token *auth.TokenData, settings *config.Settings, logCh chan<- logging.LogData) error {
	if strings.TrimSpace(transferID) == "" {
		return fmt.Errorf("transfer_id is missing")
	}
	if err := auth.Auth(settings, token, logCh); err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}
	completeURL := strings.ReplaceAll(settings.Messaging.FileActAckUrl, "{transfer-id}", url.PathEscape(transferID))
	// Sign exactly the URL and empty body sent on the wire.
	parsed, err := url.Parse(completeURL)
	if err != nil {
		return fmt.Errorf("invalid file transfer completion URL")
	}
	query := parsed.Query()
	query.Set("transfer-id", transferID)
	parsed.RawQuery = query.Encode()
	req, err := signedRequest(http.MethodPost, parsed.String(), "", token, settings)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "text/plain")
	resp, err := sendHTTP(settings.Messaging.HttpClient, req)
	if err != nil {
		return uncertainSend("file transfer completion", errors.New("request did not complete"))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return sendStatusError("file transfer completion", resp.StatusCode)
	}
	return nil
}
