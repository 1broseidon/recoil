package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/1broseidon/recoil/internal/config"
	"github.com/1broseidon/recoil/internal/scope"
	"github.com/1broseidon/recoil/internal/store"
)

func TestSetupRelayDefaultsToCollaborativePosture(t *testing.T) {
	root := t.TempDir()
	writeCmdTestFile(t, filepath.Join(root, "README.md"), "# Project\n\nCollaborative setup indexes this workspace.")
	t.Chdir(root)

	relayData := t.TempDir()
	server := httptest.NewServer(newRelayHandler(relayData))
	defer server.Close()
	invite, _, err := createRelayInvite(relayData, "agents", time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	oldOpts := opts
	opts = globalOptions{dbPath: filepath.Join(t.TempDir(), "recoil.db")}
	defer func() { opts = oldOpts }()

	c := newSetupCommand()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&bytes.Buffer{})
	c.SetArgs([]string{"--no-hooks", "--relay", server.URL + "/v1/invites/" + invite.Token})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	settings := loadTestSettings(t, root)
	assertSetting(t, settings, "channel.auto_publish", "guidance")
	assertSetting(t, settings, "channel.jit_refresh", "context")
	assertSetting(t, settings, "session-evidence.enabled", "true")
	for _, want := range []string{"posture: collaborative", "sharing: connected", "evidence: on"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("expected %q in setup output:\n%s", want, out.String())
		}
	}
}

func TestSetupManualSharePostureLeavesAutoShareOff(t *testing.T) {
	root := t.TempDir()
	writeCmdTestFile(t, filepath.Join(root, "README.md"), "# Project\n\nManual share setup indexes this workspace.")
	t.Chdir(root)

	relayData := t.TempDir()
	server := httptest.NewServer(newRelayHandler(relayData))
	defer server.Close()
	invite, _, err := createRelayInvite(relayData, "agents", time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	oldOpts := opts
	opts = globalOptions{dbPath: filepath.Join(t.TempDir(), "recoil.db")}
	defer func() { opts = oldOpts }()

	c := newSetupCommand()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&bytes.Buffer{})
	c.SetArgs([]string{"--no-hooks", "--manual-share", "--relay", server.URL + "/v1/invites/" + invite.Token})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	settings := loadTestSettings(t, root)
	assertSetting(t, settings, "channel.auto_publish", "off")
	assertSetting(t, settings, "channel.jit_refresh", "context")
	assertSetting(t, settings, "session-evidence.enabled", "false")
	if !strings.Contains(out.String(), "posture: manual-share") {
		t.Fatalf("expected manual-share setup output:\n%s", out.String())
	}
}

func TestSetupRejectsConflictingPostures(t *testing.T) {
	cases := [][]string{
		{"--standalone", "--relay", "http://127.0.0.1:8787/v1/invites/token"},
		{"--collaborative", "--manual-share", "--relay", "http://127.0.0.1:8787/v1/invites/token"},
		{"--collaborative"},
		{"--manual-share"},
	}
	for _, args := range cases {
		c := newSetupCommand()
		c.SetOut(&bytes.Buffer{})
		c.SetErr(&bytes.Buffer{})
		c.SetArgs(args)
		if err := c.Execute(); err == nil {
			t.Fatalf("expected setup %v to fail", args)
		}
	}
}

func TestSwarmStandaloneShowsLocalTree(t *testing.T) {
	root := t.TempDir()
	if _, err := scope.InitProject(root); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	out := runSwarmCommandWithDB(t, filepath.Join(t.TempDir(), "recoil.db"))
	for _, want := range []string{"tree: project", "posture: standalone", "sharing: off", "peer_memory: 0", "pending_share: 0"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in swarm output:\n%s", want, out)
		}
	}
	for _, forbidden := range []string{"channel", "outbox", "artifact"} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("swarm text leaked internal word %q:\n%s", forbidden, out)
		}
	}
}

func TestSwarmRefreshImportsPeerMemory(t *testing.T) {
	root := t.TempDir()
	if _, err := scope.InitProject(root); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	runConfigCommand(t, "set", "channel.auto_publish", "guidance")
	runConfigCommand(t, "set", "channel.jit_refresh", "context")

	relayData := t.TempDir()
	server := httptest.NewServer(newRelayHandler(relayData))
	defer server.Close()

	dbA := filepath.Join(t.TempDir(), "a.db")
	dbB := filepath.Join(t.TempDir(), "b.db")
	joinRelayForTest(t, relayData, server.URL, dbA, "alpha")
	runDecideCommandWithDB(t, dbA,
		"--claim-key", "swarm.refresh",
		"--agent", "alpha",
		"Swarm refresh imports newly shared peer memory.")
	joinRelayForTest(t, relayData, server.URL, dbB, "beta")

	out := runSwarmCommandWithDB(t, dbB, "--refresh")
	for _, want := range []string{"posture: collaborative", "sharing: connected", "peers: 1", "peer_memory: 1", "peer_memory_received: 1", "pending_share: 0"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in swarm refresh output:\n%s", want, out)
		}
	}
	for _, forbidden := range []string{"channel", "outbox", "artifact"} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("swarm text leaked internal word %q:\n%s", forbidden, out)
		}
	}
}

func TestInstructCommandsUseShortThreeVerbContract(t *testing.T) {
	for _, agent := range []string{"codex", "claude-code", "opencode"} {
		c := newInstructCommand()
		var out bytes.Buffer
		c.SetOut(&out)
		c.SetErr(&bytes.Buffer{})
		c.SetArgs([]string{agent})
		if err := c.Execute(); err != nil {
			t.Fatal(err)
		}
		got := out.String()
		for _, want := range []string{"memory tree", "recoil wake --max-chars 1600", "recoil remember --agent " + agent, "recoil handoff --agent " + agent, "shares eligible memory automatically"} {
			if !strings.Contains(got, want) {
				t.Fatalf("expected %q in instruct output for %s:\n%s", want, agent, got)
			}
		}
		for _, forbidden := range []string{"channel publish", "recoil decide", "recoil add"} {
			if strings.Contains(got, forbidden) {
				t.Fatalf("instruction text leaked old guidance %q:\n%s", forbidden, got)
			}
		}
	}

	compat := newInstructionsCommand()
	var compatOut bytes.Buffer
	compat.SetOut(&compatOut)
	compat.SetErr(&bytes.Buffer{})
	compat.SetArgs([]string{"codex"})
	if err := compat.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(compatOut.String(), "recoil handoff --agent codex") {
		t.Fatalf("expected instructions compatibility command to print contract:\n%s", compatOut.String())
	}
}

func TestRetrievalUsesPeerMemoryWordingAndKeepsJSONSourceKind(t *testing.T) {
	root := t.TempDir()
	sc, err := scope.InitProject(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	dbPath := filepath.Join(t.TempDir(), "recoil.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.AddMemory(context.Background(), store.AddMemoryParams{
		Role:        "decision",
		Content:     "Peer memory wording should make shared routing decisions readable.",
		SourceKind:  "remote_artifact",
		SourceAgent: "claude-opus",
		ScopeKind:   sc.Kind,
		ScopeID:     sc.ID,
		ProjectID:   sc.ProjectID,
		Validity:    "active",
		ClaimKey:    "peer.wording",
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	text := runSearchCommandWithDB(t, dbPath, "shared routing decisions")
	if !strings.Contains(text, "## Peer Memory") || !strings.Contains(text, "why: shared by claude-opus") {
		t.Fatalf("expected peer memory text wording:\n%s", text)
	}
	if strings.Contains(text, "Remote Artifacts") || strings.Contains(text, "synced remote artifact") {
		t.Fatalf("expected old remote artifact wording to be hidden:\n%s", text)
	}

	oldOpts := opts
	opts = globalOptions{dbPath: dbPath, json: true}
	defer func() { opts = oldOpts }()
	search := newSearchCommand()
	var out bytes.Buffer
	search.SetOut(&out)
	search.SetErr(&bytes.Buffer{})
	search.SetArgs([]string{"shared routing decisions"})
	if err := search.Execute(); err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Data struct {
			Lanes []retrievalLaneResult `json:"lanes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Data.Lanes) != 1 || envelope.Data.Lanes[0].Key != "remote_artifacts" || envelope.Data.Lanes[0].Title != "Peer Memory" {
		t.Fatalf("expected compatibility lane key with peer title, got %+v", envelope.Data.Lanes)
	}
	if got := envelope.Data.Lanes[0].Results[0].SourceKind; got != "remote_artifact" {
		t.Fatalf("expected JSON source_kind compatibility, got %q", got)
	}
}

func TestWakeShowsSwarmPresenceAndPeerMemoryWording(t *testing.T) {
	root := t.TempDir()
	if _, err := scope.InitProject(root); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	runConfigCommand(t, "set", "channel.auto_publish", "guidance")
	runConfigCommand(t, "set", "channel.jit_refresh", "context")

	relayData := t.TempDir()
	server := httptest.NewServer(newRelayHandler(relayData))
	defer server.Close()

	dbA := filepath.Join(t.TempDir(), "a.db")
	dbB := filepath.Join(t.TempDir(), "b.db")
	joinRelayForTest(t, relayData, server.URL, dbA, "alpha")
	runDecideCommandWithDB(t, dbA,
		"--claim-key", "wake.presence",
		"--agent", "alpha",
		"Wake should show swarm presence and shared peer memory wording.")
	joinRelayForTest(t, relayData, server.URL, dbB, "beta")

	out := runWakeCommandWithDB(t, dbB, "swarm presence shared peer", "--limit", "5")
	for _, want := range []string{"swarm: 1 peers; 1 peer memories received; 0 pending share", "peer_memory_received: 1", "## Peer Memory", "why: shared by alpha"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in wake output:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Remote Artifacts") || strings.Contains(out, "synced remote artifact") {
		t.Fatalf("expected old remote artifact wording to be hidden:\n%s", out)
	}
}

func joinRelayForTest(t *testing.T, relayData, serverURL, dbPath, agent string) {
	t.Helper()
	invite, _, err := createRelayInvite(relayData, "agents", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	runChannelCommandWithDB(t, dbPath, "join", "--agent", agent, serverURL+"/v1/invites/"+invite.Token)
}

func runSwarmCommandWithDB(t *testing.T, dbPath string, args ...string) string {
	t.Helper()
	oldOpts := opts
	opts = globalOptions{dbPath: dbPath}
	defer func() { opts = oldOpts }()

	c := newSwarmCommand()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&bytes.Buffer{})
	c.SetArgs(args)
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func runWakeCommandWithDB(t *testing.T, dbPath string, args ...string) string {
	t.Helper()
	oldOpts := opts
	opts = globalOptions{dbPath: dbPath}
	defer func() { opts = oldOpts }()

	c := newWakeCommand()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&bytes.Buffer{})
	c.SetArgs(args)
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func loadTestSettings(t *testing.T, root string) config.Settings {
	t.Helper()
	path, err := config.ResolveSettingsPath(root)
	if err != nil {
		t.Fatal(err)
	}
	settings, err := config.LoadSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	return settings
}

func assertSetting(t *testing.T, settings config.Settings, key, want string) {
	t.Helper()
	got, ok := settings.Get(key)
	if !ok || got != want {
		t.Fatalf("expected setting %s=%q, got %q found=%t", key, want, got, ok)
	}
}
