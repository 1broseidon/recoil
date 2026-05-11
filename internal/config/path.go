package config

import (
	"os"
	"path/filepath"
	"strings"
)

func ResolveDBPath(flagPath string) (string, error) {
	if flagPath != "" {
		return cleanPath(flagPath)
	}
	if envPath := os.Getenv("RECOIL_DB"); envPath != "" {
		return cleanPath(envPath)
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "recoil", "recoil.db"), nil
}

func ResolveStateDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "recoil"), nil
}

func ResolveUserIDPath() (string, error) {
	dir, err := ResolveStateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "user_id"), nil
}

func PathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func cleanPath(path string) (string, error) {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
	}
	return filepath.Abs(path)
}
