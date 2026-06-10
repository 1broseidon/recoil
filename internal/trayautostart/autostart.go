package trayautostart

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
)

type Config struct {
	AppID      string
	Name       string
	Executable string
	Arguments  []string
}

type Status struct {
	Platform  string   `json:"platform"`
	Installed bool     `json:"installed"`
	Path      string   `json:"path"`
	Command   []string `json:"command"`
	Message   string   `json:"message"`
}

func Install(config Config) (Status, error) {
	normalized, err := normalize(config)
	if err != nil {
		return Status{}, err
	}
	return install(normalized)
}

func Check(config Config) (Status, error) {
	normalized, err := normalize(config)
	if err != nil {
		return Status{}, err
	}
	return check(normalized)
}

func Uninstall(config Config) (Status, error) {
	normalized, err := normalize(config)
	if err != nil {
		return Status{}, err
	}
	return uninstall(normalized)
}

func normalize(config Config) (Config, error) {
	config.AppID = strings.TrimSpace(config.AppID)
	config.Name = strings.TrimSpace(config.Name)
	config.Executable = strings.TrimSpace(config.Executable)
	if config.AppID == "" {
		return Config{}, errors.New("autostart app id is required")
	}
	if config.Name == "" {
		return Config{}, errors.New("autostart name is required")
	}
	if config.Executable == "" {
		return Config{}, errors.New("autostart executable is required")
	}
	exe, err := filepath.Abs(config.Executable)
	if err != nil {
		return Config{}, fmt.Errorf("resolve executable: %w", err)
	}
	config.Executable = exe
	config.Arguments = cleanArgs(config.Arguments)
	return config, nil
}

func cleanArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		if arg == "" {
			continue
		}
		out = append(out, arg)
	}
	return out
}

func command(config Config) []string {
	out := []string{config.Executable}
	out = append(out, config.Arguments...)
	return out
}

func baseStatus(config Config, path string, installed bool, message string) Status {
	return Status{
		Platform:  runtime.GOOS,
		Installed: installed,
		Path:      path,
		Command:   command(config),
		Message:   message,
	}
}
