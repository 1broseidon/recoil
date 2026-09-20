//go:build linux

package trayautostart

import (
	"os"
	"path/filepath"
	"strings"
)

func install(config Config) (Status, error) {
	path, err := linuxAutostartPath()
	if err != nil {
		return Status{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return Status{}, err
	}
	body := linuxDesktopEntry(config)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return Status{}, err
	}
	return baseStatus(config, path, true, "installed"), nil
}

func check(config Config) (Status, error) {
	path, err := linuxAutostartPath()
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
	path, err := linuxAutostartPath()
	if err != nil {
		return Status{}, err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return Status{}, err
	}
	return baseStatus(config, path, false, "uninstalled"), nil
}

func linuxAutostartPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "autostart", "recoil-tray.desktop"), nil
}

func linuxDesktopEntry(config Config) string {
	return strings.Join([]string{
		"[Desktop Entry]",
		"Type=Application",
		"Version=1.0",
		"Name=" + config.Name,
		"Comment=Ambient Recoil memory status",
		"Exec=" + desktopExec(command(config)),
		"Terminal=false",
		"X-GNOME-Autostart-enabled=true",
		"",
	}, "\n")
}

func desktopExec(args []string) string {
	parts := make([]string, 0, len(args))
	for _, arg := range args {
		parts = append(parts, desktopQuote(arg))
	}
	return strings.Join(parts, " ")
}

func desktopQuote(arg string) string {
	if !strings.ContainsAny(arg, " \t\n\"'\\") {
		return arg
	}
	arg = strings.ReplaceAll(arg, "\\", "\\\\")
	arg = strings.ReplaceAll(arg, "\"", "\\\"")
	return `"` + arg + `"`
}
