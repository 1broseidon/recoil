//go:build !linux && !darwin && !windows

package trayautostart

import "fmt"

func install(config Config) (Status, error) {
	return Status{}, unsupported()
}

func check(config Config) (Status, error) {
	return Status{}, unsupported()
}

func uninstall(config Config) (Status, error) {
	return Status{}, unsupported()
}

func unsupported() error {
	return fmt.Errorf("tray autostart is not supported on this OS")
}
