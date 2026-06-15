package config

import (
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
	var partners []Partner
	if err := json.Unmarshal(data, &partners); err != nil {
		return nil, err
	}
	return partners, nil
}
