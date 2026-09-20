//go:build windows

package trayautostart

import (
	"os"
	"path/filepath"
	"strings"
)

func install(config Config) (Status, error) {
	path, err := windowsStartupPath(config)
	if err != nil {
		return Status{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return Status{}, err
	}
	if err := os.WriteFile(path, []byte(windowsLauncherScript(config)), 0o644); err != nil {
		return Status{}, err
	}
	return baseStatus(config, path, true, "installed"), nil
}

func check(config Config) (Status, error) {
	path, err := windowsStartupPath(config)
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
	path, err := windowsStartupPath(config)
	if err != nil {
		return Status{}, err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return Status{}, err
	}
	return baseStatus(config, path, false, "uninstalled"), nil
}

func windowsStartupPath(config Config) (string, error) {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		dir, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		appData = dir
	}
	return filepath.Join(appData, "Microsoft", "Windows", "Start Menu", "Programs", "Startup", config.Name+".vbs"), nil
}

func windowsLauncherScript(config Config) string {
	return strings.Join([]string{
		`Set shell = CreateObject("WScript.Shell")`,
		`shell.Run ` + vbsString(commandLine(command(config))) + `, 0, False`,
		"",
	}, "\r\n")
}

func commandLine(args []string) string {
	parts := make([]string, 0, len(args))
	for _, arg := range args {
		parts = append(parts, windowsArg(arg))
	}
	return strings.Join(parts, " ")
}

func windowsArg(arg string) string {
	if !strings.ContainsAny(arg, " \t\"") {
		return arg
	}
	return `"` + strings.ReplaceAll(arg, `"`, `\"`) + `"`
}

func vbsString(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}
