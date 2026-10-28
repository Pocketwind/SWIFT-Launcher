package config

import (
	"fmt"
	"os"

	json "github.com/json-iterator/go"
)

func LoadSettings(path string) (*Settings, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var settings Settings
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, err
	}
	return &settings, nil
}

func LoadPartners(path string) ([]Partner, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	if entries == nil {
		return nil, nil
	}
	partners := make([]Partner, len(entries))
	for i, entry := range entries {
		// Decode over the default to distinguish an omitted setting from an
		// explicit false without changing Partner's public bool field.
		partners[i].ArchiveEnabled = true
		if err := json.Unmarshal(entry, &partners[i]); err != nil {
			return nil, fmt.Errorf("partner %d: %w", i+1, err)
		}
	}
	return partners, nil
}
