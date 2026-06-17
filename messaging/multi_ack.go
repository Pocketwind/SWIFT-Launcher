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
		logging.Easylog(logCh, "ERROR", fmt.Sprintf("Error making request: %v", err))
		return err
	}
	defer resp.Body.Close()

	return nil
}
