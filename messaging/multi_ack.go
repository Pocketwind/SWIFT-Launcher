package messaging

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/Pocketwind/SWIFT-Launcher/auth"
	"github.com/Pocketwind/SWIFT-Launcher/config"
	"github.com/Pocketwind/SWIFT-Launcher/logging"
)

func MultiAck(settings *config.Settings, tokenData *auth.TokenData, ids []string, logCh chan<- logging.LogData) error {
	if len(ids) == 0 {
		return nil
	}
	//Auth
	if err := auth.Auth(settings, tokenData, logCh); err != nil {
		return fmt.Errorf("authenticate ACK: %w", err)
	}
	tokenData.RLock()
	accessToken := tokenData.AccessToken
	tokenType := tokenData.TokenType
	tokenData.RUnlock()
	//URL
	ackUrl := settings.Messaging.DistributionUrl

	type ackItem struct {
		ID     int    `json:"id"`
		Status string `json:"status"`
	}

	ackList := make([]ackItem, 0, len(ids))
	for _, id := range ids {
		parsedID, err := strconv.Atoi(id)
		if err != nil {
			return fmt.Errorf("invalid id %q: %w", id, err)
		}
		if parsedID <= 0 {
			return fmt.Errorf("invalid distribution ID %d", parsedID)
		}
		ackList = append(ackList, ackItem{ID: parsedID, Status: "Ack"})
	}

	body, err := json.Marshal(ackList)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("PATCH", ackUrl, bytes.NewBuffer(body))
	if err != nil {
		return err
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("%s %s", tokenType, accessToken))
	req.Header.Set("Content-Type", "application/json")

	client := settings.Messaging.HttpClient
	if client == nil {
		return fmt.Errorf("messaging HTTP client is not configured")
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("error making request: %w", err)
	}
	defer resp.Body.Close()

	//200이 OK, 나머지는 에러
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Failed to ack messages. HTTP status: %d", resp.StatusCode))
		return fmt.Errorf("failed to ack messages: HTTP %d", resp.StatusCode)
	} else {
		logging.Easylog(logCh, "INFO", fmt.Sprintf("Acked %d messages", len(ids)))
	}

	return nil
}
