package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Settings struct {
	Values map[string]string `json:"values"`
}

func ResolveSettingsPath(projectRoot string) (string, error) {
	if strings.TrimSpace(projectRoot) != "" {
		return filepath.Join(projectRoot, ".recoil", "config.json"), nil
	}
	dir, err := ResolveStateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

func LoadSettings(path string) (Settings, error) {
	settings := Settings{Values: map[string]string{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return settings, nil
		}
		return settings, err
	}
	if len(data) == 0 {
		return settings, nil
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		return settings, err
	}
	if settings.Values == nil {
		settings.Values = map[string]string{}
	}
	return settings, nil
}

func SaveSettings(path string, settings Settings) error {
	if settings.Values == nil {
		settings.Values = map[string]string{}
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func (s Settings) Get(key string) (string, bool) {
	value, ok := s.Values[strings.TrimSpace(key)]
	return value, ok
}

func (s *Settings) Set(key, value string) {
	if s.Values == nil {
		s.Values = map[string]string{}
	}
	s.Values[strings.TrimSpace(key)] = strings.TrimSpace(value)
}

func (s Settings) Bool(key string, def bool) bool {
	raw, ok := s.Get(key)
	if !ok || strings.TrimSpace(raw) == "" {
		return def
	}
	value, err := strconv.ParseBool(strings.TrimSpace(raw))
	if err != nil {
		return def
	}
	return value
}

func (s Settings) Int(key string, def int) int {
	raw, ok := s.Get(key)
	if !ok || strings.TrimSpace(raw) == "" {
		return def
	}
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return def
	}
	return value
}
