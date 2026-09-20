//go:build darwin

package trayautostart

import (
	"bytes"
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
)

func install(config Config) (Status, error) {
	path, err := darwinLaunchAgentPath(config)
	if err != nil {
		return Status{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return Status{}, err
	}
	if err := os.WriteFile(path, []byte(darwinLaunchAgentPlist(config)), 0o644); err != nil {
		return Status{}, err
	}
	return baseStatus(config, path, true, "installed"), nil
}

func check(config Config) (Status, error) {
	path, err := darwinLaunchAgentPath(config)
	if err != nil {
		return Status{}, err
	}
	_, err = os.Stat(path)
	if err == nil {
		return baseStatus(config, path, true, "installed"), nil
	}
	if os.IsNotExist(err) {
		return baseStatus(config, path, false, "not installed"), nil
	}
	return Status{}, err
}

func uninstall(config Config) (Status, error) {
	path, err := darwinLaunchAgentPath(config)
	if err != nil {
		return Status{}, err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return Status{}, err
	}
	return baseStatus(config, path, false, "uninstalled"), nil
}

func darwinLaunchAgentPath(config Config) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", config.AppID+".plist"), nil
}

func darwinLaunchAgentPlist(config Config) string {
	args := command(config)
	var argItems []string
	for _, arg := range args {
		argItems = append(argItems, "\t\t<string>"+xmlText(arg)+"</string>")
	}
	return strings.Join([]string{
		`<?xml version="1.0" encoding="UTF-8"?>`,
		`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">`,
		`<plist version="1.0">`,
		`<dict>`,
		"\t<key>Label</key>",
		"\t<string>" + xmlText(config.AppID) + "</string>",
		"\t<key>ProgramArguments</key>",
		"\t<array>",
		strings.Join(argItems, "\n"),
		"\t</array>",
		"\t<key>RunAtLoad</key>",
		"\t<true/>",
		"\t<key>KeepAlive</key>",
		"\t<false/>",
		"\t<key>LimitLoadToSessionType</key>",
		"\t<string>Aqua</string>",
		`</dict>`,
		`</plist>`,
		"",
	}, "\n")
}

func xmlText(value string) string {
	var buf bytes.Buffer
	_ = xml.EscapeText(&buf, []byte(value))
	return buf.String()
}
