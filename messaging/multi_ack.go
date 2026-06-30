package messaging

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/Pocketwind/SWIFT-Launcher/auth"
	"github.com/Pocketwind/SWIFT-Launcher/config"
	"github.com/Pocketwind/SWIFT-Launcher/logging"
)

func MultiAck(settings *config.Settings, tokenData *auth.TokenData, ids []string, logCh chan<- logging.LogData) error {
	//Auth
	auth.Auth(settings, tokenData, logCh)
	tokenData.RLock()
	accessToken := tokenData.AccessToken
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
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))
	req.Header.Set("Content-Type", "application/json")

	client := settings.Messaging.HttpClient

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("error making request: %w", err)
	}
	defer resp.Body.Close()

	//200이 OK, 나머지는 에러
	if resp.StatusCode != 200 {
		errorBody, _ := io.ReadAll(resp.Body)
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Failed to ack messages. Status: %d, Response: %s", resp.StatusCode, string(errorBody)))
		return fmt.Errorf("failed to ack messages. Status: %d, Response: %s", resp.StatusCode, string(errorBody))
	} else {
		logging.Easylog(logCh, "INFO", fmt.Sprintf("Acked %d messages", len(ids)))
	}

	return nil
}
