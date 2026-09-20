//go:build linux

package trayautostart

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLinuxInstallStatusUninstall(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)

	config := Config{
		AppID:      "com.1broseidon.recoil.tray",
		Name:       "Recoil Tray",
		Executable: filepath.Join(t.TempDir(), "recoil"),
		Arguments:  []string{"tray", "--all-scopes", "--interval", "15s"},
	}
	status, err := Install(config)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Installed {
		t.Fatalf("expected installed status: %#v", status)
	}
	if want := filepath.Join(configHome, "autostart", "recoil-tray.desktop"); status.Path != want {
		t.Fatalf("path = %q, want %q", status.Path, want)
	}
	body, err := os.ReadFile(status.Path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{
		"Type=Application",
		"Name=Recoil Tray",
		"Exec=",
		" tray --all-scopes --interval 15s",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %q in desktop entry:\n%s", want, text)
		}
	}

	status, err = Check(config)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Installed {
		t.Fatalf("expected installed check status: %#v", status)
	}

	status, err = Uninstall(config)
	if err != nil {
		t.Fatal(err)
	}
	if status.Installed {
		t.Fatalf("expected uninstalled status: %#v", status)
	}
	if _, err := os.Stat(status.Path); !os.IsNotExist(err) {
		t.Fatalf("expected autostart file removed, stat err=%v", err)
	}
}

func TestDesktopExecQuotesArguments(t *testing.T) {
	got := desktopExec([]string{"/tmp/Recoil Bin/recoil", "tray", "--project", "/tmp/project with spaces"})
	if want := `"/tmp/Recoil Bin/recoil" tray --project "/tmp/project with spaces"`; got != want {
		t.Fatalf("desktopExec = %q, want %q", got, want)
	}
}
