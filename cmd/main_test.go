package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain points every HOME-derived path at a throwaway directory before any
// test runs, so the suite can never open, migrate, or mine into the developer's
// real store under os.UserConfigDir (~/.config/recoil on Linux, Library/
// Application Support on macOS, %AppData% on Windows). Worktree-aware scoping
// maps a checkout of this repo to its own project, which is exactly how a test
// run once re-mined the README into a live store. Tests that need a specific
// HOME still set their own with t.Setenv.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "recoil-cmd-test-")
	if err != nil {
		panic(err)
	}
	for key, value := range map[string]string{
		"HOME":            home,
		"XDG_CONFIG_HOME": filepath.Join(home, ".config"),
		"XDG_DATA_HOME":   filepath.Join(home, ".local", "share"),
		"APPDATA":         filepath.Join(home, "AppData", "Roaming"),
		"LOCALAPPDATA":    filepath.Join(home, "AppData", "Local"),
	} {
		_ = os.Setenv(key, value)
	}
	// Aiming variables from the developer's shell must not steer the suite.
	_ = os.Unsetenv("RECOIL_DB")
	_ = os.Unsetenv("RECOIL_PROJECT")
	code := m.Run()
	_ = os.RemoveAll(home)
	os.Exit(code)
}
