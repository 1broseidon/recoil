package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	channelpkg "github.com/1broseidon/recoil/internal/channel"
	"github.com/1broseidon/recoil/internal/scope"
	"github.com/1broseidon/recoil/internal/store"
)

func TestChannelPublishesAndSyncsBetweenTwoDatabases(t *testing.T) {
	channelDir := t.TempDir()
	scopeID := "channel-demo"
	dbA := filepath.Join(t.TempDir(), "a.db")
	dbB := filepath.Join(t.TempDir(), "b.db")

	ctx := context.Background()
	stA, err := store.Open(dbA)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := stA.AddMemory(ctx, store.AddMemoryParams{
		Role:      "decision",
		Content:   "Distributed memory uses pubsub channels to move signed artifacts between local Recoil nodes.",
		ScopeKind: "session",
		ScopeID:   scopeID,
		Validity:  "active",
		ClaimKey:  "distributed-memory.pubsub",
	}); err != nil {
		t.Fatal(err)
	}
	if err := stA.Close(); err != nil {
		t.Fatal(err)
	}

	runChannelCommandWithDB(t, dbA, "join", "--session", scopeID, "--agent", "alpha", "--name", "demo", channelDir)
	publish := runChannelCommandWithDB(t, dbA, "publish")
	if !strings.Contains(publish, "published: 1") {
		t.Fatalf("expected one published artifact, got:\n%s", publish)
	}

	joinB := runChannelCommandWithDB(t, dbB, "join", "--session", scopeID, "--agent", "beta", channelDir)
	for _, want := range []string{"roster_count: 2", "artifact_count: 1", "agent=alpha", "agent=beta"} {
		if !strings.Contains(joinB, want) {
			t.Fatalf("expected %q in join output:\n%s", want, joinB)
		}
	}

	sync := runChannelCommandWithDB(t, dbB, "sync")
	if !strings.Contains(sync, "imported: 1") {
		t.Fatalf("expected one imported artifact, got:\n%s", sync)
	}

	stB, err := store.Open(dbB)
	if err != nil {
		t.Fatal(err)
	}
	defer stB.Close()
	results, err := stB.Search(ctx, store.SearchParams{
		Query:     "pubsub signed artifacts",
		ScopeKind: "session",
		ScopeID:   scopeID,
		Limit:     5,
		Lifecycle: store.LifecycleCurrent,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected one synced memory, got %+v", results)
	}
	got := results[0]
	if got.SourceKind != "remote_artifact" || got.SourceAgent != "alpha" {
		t.Fatalf("expected remote alpha artifact, got source_kind=%q source_agent=%q", got.SourceKind, got.SourceAgent)
	}
	if got.ClaimKey != "distributed-memory.pubsub" || !strings.Contains(got.MetadataJSON, `"remote_artifact"`) {
		t.Fatalf("expected remote provenance metadata, got %+v", got)
	}

	secondSync := runChannelCommandWithDB(t, dbB, "sync")
	if !strings.Contains(secondSync, "skipped_duplicate: 1") {
		t.Fatalf("expected duplicate sync to skip existing event, got:\n%s", secondSync)
	}
}

func TestChannelDoesNotImportSelfPublishedArtifacts(t *testing.T) {
	channelDir := t.TempDir()
	scopeID := "self-import"
	dbPath := filepath.Join(t.TempDir(), "recoil.db")

	ctx := context.Background()
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.AddMemory(ctx, store.AddMemoryParams{
		Role:      "decision",
		Content:   "Self published channel artifacts are not re-imported.",
		ScopeKind: "session",
		ScopeID:   scopeID,
		Validity:  "active",
		ClaimKey:  "distributed-memory.self-import",
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	runChannelCommandWithDB(t, dbPath, "join", "--session", scopeID, "--agent", "alpha", channelDir)
	runChannelCommandWithDB(t, dbPath, "publish")
	sync := runChannelCommandWithDB(t, dbPath, "sync")
	if !strings.Contains(sync, "skipped_self: 1") || strings.Contains(sync, "imported: 1") {
		t.Fatalf("expected self artifact skip, got:\n%s", sync)
	}
}

func TestRelayPublishesAndSyncsBetweenTwoDatabases(t *testing.T) {
	relayData := t.TempDir()
	server := httptest.NewServer(newRelayHandler(relayData))
	defer server.Close()

	scopeID := "relay-demo"
	dbA := filepath.Join(t.TempDir(), "a.db")
	dbB := filepath.Join(t.TempDir(), "b.db")

	ctx := context.Background()
	stA, err := store.Open(dbA)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := stA.AddMemory(ctx, store.AddMemoryParams{
		Role:      "decision",
		Content:   "Relay transport moves signed artifacts between machines without syncing local databases.",
		ScopeKind: "session",
		ScopeID:   scopeID,
		Validity:  "active",
		ClaimKey:  "distributed-memory.relay",
	}); err != nil {
		t.Fatal(err)
	}
	if err := stA.Close(); err != nil {
		t.Fatal(err)
	}

	inviteA, manifest, err := createRelayInvite(relayData, "agents", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	joinA := runChannelCommandWithDB(t, dbA, "join", "--session", scopeID, "--agent", "alpha", server.URL+"/v1/invites/"+inviteA.Token)
	if !strings.Contains(joinA, manifest.ChannelID) || !strings.Contains(joinA, "roster_count: 1") {
		t.Fatalf("expected alpha join output, got:\n%s", joinA)
	}
	publish := runChannelCommandWithDB(t, dbA, "publish")
	if !strings.Contains(publish, "published: 1") {
		t.Fatalf("expected alpha publish, got:\n%s", publish)
	}

	inviteB, _, err := createRelayInvite(relayData, "agents", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	joinB := runChannelCommandWithDB(t, dbB, "join", "--session", scopeID, "--agent", "beta", server.URL+"/v1/invites/"+inviteB.Token)
	for _, want := range []string{"roster_count: 2", "artifact_count: 1", "agent=alpha", "agent=beta"} {
		if !strings.Contains(joinB, want) {
			t.Fatalf("expected %q in beta join output:\n%s", want, joinB)
		}
	}

	sync := runChannelCommandWithDB(t, dbB, "sync")
	if !strings.Contains(sync, "imported: 1") {
		t.Fatalf("expected beta sync import, got:\n%s", sync)
	}

	stB, err := store.Open(dbB)
	if err != nil {
		t.Fatal(err)
	}
	defer stB.Close()
	results, err := stB.Search(ctx, store.SearchParams{
		Query:      "relay signed artifacts machines",
		ScopeKind:  "session",
		ScopeID:    scopeID,
		SourceKind: "remote_artifact",
		Limit:      5,
		Lifecycle:  store.LifecycleCurrent,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].SourceAgent != "alpha" {
		t.Fatalf("expected one remote alpha artifact, got %+v", results)
	}
}

func TestRelayRefreshImportsNewArtifactsAndReportsPeerDelta(t *testing.T) {
	relayData := t.TempDir()
	server := httptest.NewServer(newRelayHandler(relayData))
	defer server.Close()

	scopeID := "relay-refresh"
	dbA := filepath.Join(t.TempDir(), "a.db")
	dbB := filepath.Join(t.TempDir(), "b.db")
	ctx := context.Background()

	stA, err := store.Open(dbA)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := stA.AddMemory(ctx, store.AddMemoryParams{
		Role:      "decision",
		Content:   "Relay refresh imports peer artifacts before context composition.",
		ScopeKind: "session",
		ScopeID:   scopeID,
		Validity:  "active",
		ClaimKey:  "relay.refresh",
	}); err != nil {
		t.Fatal(err)
	}
	if err := stA.Close(); err != nil {
		t.Fatal(err)
	}

	inviteA, _, err := createRelayInvite(relayData, "agents", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	runChannelCommandWithDB(t, dbA, "join", "--session", scopeID, "--agent", "alpha", server.URL+"/v1/invites/"+inviteA.Token)
	runChannelCommandWithDB(t, dbA, "publish", "--claim-key", "relay.refresh")

	inviteB, _, err := createRelayInvite(relayData, "agents", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	runChannelCommandWithDB(t, dbB, "join", "--session", scopeID, "--agent", "beta", server.URL+"/v1/invites/"+inviteB.Token)

	refresh := runChannelCommandWithDB(t, dbB, "refresh")
	for _, want := range []string{"imported: 1", "new_peers: 1", "agent=alpha"} {
		if !strings.Contains(refresh, want) {
			t.Fatalf("expected %q in refresh output:\n%s", want, refresh)
		}
	}
	second := runChannelCommandWithDB(t, dbB, "refresh")
	if !strings.Contains(second, "imported: 0") || !strings.Contains(second, "new_peers: 0") {
		t.Fatalf("expected cursor-backed no-op refresh, got:\n%s", second)
	}
}

func TestChannelPublishPrecisionDryRunAndClaimKey(t *testing.T) {
	channelDir := t.TempDir()
	scopeID := "publish-precision"
	dbPath := filepath.Join(t.TempDir(), "recoil.db")
	ctx := context.Background()

	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.AddMemory(ctx, store.AddMemoryParams{
		Role:      "decision",
		Content:   "Publish this exact claim-keyed decision.",
		ScopeKind: "session",
		ScopeID:   scopeID,
		Validity:  "active",
		ClaimKey:  "publish.precise",
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.AddMemory(ctx, store.AddMemoryParams{
		Role:      "decision",
		Content:   "This decision intentionally lacks a claim key.",
		ScopeKind: "session",
		ScopeID:   scopeID,
		Validity:  "active",
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	runChannelCommandWithDB(t, dbPath, "join", "--session", scopeID, "--agent", "alpha", "--name", "demo", channelDir)
	preview := runChannelCommandWithDB(t, dbPath, "publish", "--dry-run")
	if !strings.Contains(preview, "planned: 1") || !strings.Contains(preview, "dry_run: true") {
		t.Fatalf("expected default dry-run to plan only claim-keyed memory, got:\n%s", preview)
	}
	events, err := channelpkg.ReadEvents(channelDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("dry-run wrote %d events", len(events))
	}
	publish := runChannelCommandWithDB(t, dbPath, "publish", "--claim-key", "publish.precise")
	if !strings.Contains(publish, "published: 1") {
		t.Fatalf("expected claim-key publish, got:\n%s", publish)
	}
}

func TestAutoPublishGuidanceAndSearchJITRefresh(t *testing.T) {
	root := t.TempDir()
	if _, err := scope.InitProject(root); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	runConfigCommand(t, "set", "channel.auto_publish", "guidance")

	relayData := t.TempDir()
	server := httptest.NewServer(newRelayHandler(relayData))
	defer server.Close()

	scopeID := "auto-publish"
	dbA := filepath.Join(t.TempDir(), "a.db")
	dbB := filepath.Join(t.TempDir(), "b.db")
	inviteA, _, err := createRelayInvite(relayData, "agents", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	runChannelCommandWithDB(t, dbA, "join", "--session", scopeID, "--agent", "alpha", server.URL+"/v1/invites/"+inviteA.Token)

	decide := runDecideCommandWithDB(t, dbA,
		"--session", scopeID,
		"--claim-key", "auto.publish.guidance",
		"Auto publish should share claim-keyed guidance over the relay.")
	for _, want := range []string{"publish_mode: guidance", "publish_published: 1", "publish_queued: 1"} {
		if !strings.Contains(decide, want) {
			t.Fatalf("expected %q in decide output:\n%s", want, decide)
		}
	}

	inviteB, _, err := createRelayInvite(relayData, "agents", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	runChannelCommandWithDB(t, dbB, "join", "--session", scopeID, "--agent", "beta", server.URL+"/v1/invites/"+inviteB.Token)
	search := runSearchCommandWithDB(t, dbB, "claim-keyed guidance relay", "--session", scopeID, "--limit", "5")
	for _, want := range []string{"channel_imported: 1", "source_kind: remote_artifact", "source_agent: alpha"} {
		if !strings.Contains(search, want) {
			t.Fatalf("expected %q in search output:\n%s", want, search)
		}
	}
}

func TestAutoPublishQueuesOutboxWhenRelayUnavailable(t *testing.T) {
	root := t.TempDir()
	if _, err := scope.InitProject(root); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	runConfigCommand(t, "set", "channel.auto_publish", "guidance")
	runConfigCommand(t, "set", "channel.outbox_flush_timeout", "50ms")

	scopeID := "offline-outbox"
	dbPath := filepath.Join(t.TempDir(), "recoil.db")
	ctx := context.Background()
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := st.GetOrCreateChannelIdentity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertChannelSubscription(ctx, store.UpsertChannelSubscriptionParams{
		ChannelID: "ch_offline",
		Name:      "offline",
		Path:      "http://127.0.0.1:1",
		NodeID:    identity.NodeID,
		Agent:     "alpha",
		ScopeKind: "session",
		ScopeID:   scopeID,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	decide := runDecideCommandWithDB(t, dbPath,
		"--session", scopeID,
		"--claim-key", "auto.publish.offline",
		"Auto publish should queue locally when the relay is unavailable.")
	for _, want := range []string{"publish_mode: guidance", "publish_queued: 1", "publish_failed: 1"} {
		if !strings.Contains(decide, want) {
			t.Fatalf("expected %q in decide output:\n%s", want, decide)
		}
	}
	outbox := runChannelCommandWithDB(t, dbPath, "outbox")
	for _, want := range []string{"pending: 1", "ch_offline", "attempts=1"} {
		if !strings.Contains(outbox, want) {
			t.Fatalf("expected %q in outbox output:\n%s", want, outbox)
		}
	}
}

func TestRelayRequiresSignedRequests(t *testing.T) {
	relayData := t.TempDir()
	server := httptest.NewServer(newRelayHandler(relayData))
	defer server.Close()

	_, manifest, err := createRelayInvite(relayData, "secure", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get(server.URL + "/v1/channels/" + manifest.ChannelID + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected unsigned event read to be rejected, got %s", resp.Status)
	}
}

func TestRelayInviteIsSingleUseUnderConcurrentJoins(t *testing.T) {
	relayData := t.TempDir()
	server := httptest.NewServer(newRelayHandler(relayData))
	defer server.Close()

	invite, _, err := createRelayInvite(relayData, "concurrent", time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	stA, err := store.Open(filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer stA.Close()
	stB, err := store.Open(filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer stB.Close()

	idA, err := stA.GetOrCreateChannelIdentity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	idB, err := stB.GetOrCreateChannelIdentity(ctx)
	if err != nil {
		t.Fatal(err)
	}

	cardFor := func(id store.ChannelIdentity, agent string) []byte {
		ch := store.ChannelSubscription{
			ChannelID: invite.ChannelID,
			NodeID:    id.NodeID,
			Agent:     agent,
			ScopeKind: "session",
			ScopeID:   "concurrent",
		}
		card := channelpkg.BuildRosterCard(ch, id)
		if err := channelpkg.SignRosterCard(&card, id.PrivateKey); err != nil {
			t.Fatal(err)
		}
		body, err := json.Marshal(card)
		if err != nil {
			t.Fatal(err)
		}
		return body
	}

	bodyA := cardFor(idA, "alpha")
	bodyB := cardFor(idB, "beta")
	url := server.URL + "/v1/invites/" + invite.Token + "/join"

	var wg sync.WaitGroup
	var statusA, statusB int
	wg.Add(2)
	go func() {
		defer wg.Done()
		resp, err := http.Post(url, "application/json", bytes.NewReader(bodyA))
		if err != nil {
			t.Errorf("alpha post: %v", err)
			return
		}
		defer resp.Body.Close()
		statusA = resp.StatusCode
	}()
	go func() {
		defer wg.Done()
		resp, err := http.Post(url, "application/json", bytes.NewReader(bodyB))
		if err != nil {
			t.Errorf("beta post: %v", err)
			return
		}
		defer resp.Body.Close()
		statusB = resp.StatusCode
	}()
	wg.Wait()

	ok, fail := 0, 0
	for _, s := range []int{statusA, statusB} {
		if s == http.StatusOK {
			ok++
		} else if s == http.StatusForbidden {
			fail++
		} else {
			t.Fatalf("unexpected status %d (alpha=%d beta=%d)", s, statusA, statusB)
		}
	}
	if ok != 1 || fail != 1 {
		t.Fatalf("expected exactly one join to succeed; got alpha=%d beta=%d", statusA, statusB)
	}

	resp, err := http.Post(url, "application/json", bytes.NewReader(bodyA))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("expected a third join to be rejected, got %s", resp.Status)
	}
}

func TestRelayOperatorLifecycleCommands(t *testing.T) {
	relayData := t.TempDir()
	invite, manifest, err := createRelayInvite(relayData, "ops", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	status := runRelayCommand(t, "status", "--data", relayData)
	for _, want := range []string{"channel_count: 1", "outstanding_invites: 1"} {
		if !strings.Contains(status, want) {
			t.Fatalf("expected %q in status output:\n%s", want, status)
		}
	}
	inviteStatus := runRelayCommand(t, "invite", "status", "--data", relayData, invite.Token)
	if !strings.Contains(inviteStatus, "status: outstanding") {
		t.Fatalf("expected outstanding invite, got:\n%s", inviteStatus)
	}
	revoked := runRelayCommand(t, "invite", "revoke", "--data", relayData, invite.Token)
	if !strings.Contains(revoked, "status: revoked") {
		t.Fatalf("expected revoked invite, got:\n%s", revoked)
	}
	inspect := runRelayCommand(t, "channel", "inspect", "--data", relayData, manifest.ChannelID)
	if !strings.Contains(inspect, "name: ops") {
		t.Fatalf("expected inspected channel, got:\n%s", inspect)
	}
}

func runChannelCommandWithDB(t *testing.T, dbPath string, args ...string) string {
	t.Helper()
	oldOpts := opts
	opts = globalOptions{dbPath: dbPath}
	defer func() { opts = oldOpts }()

	c := newChannelCommand()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&bytes.Buffer{})
	c.SetArgs(args)
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func runDecideCommandWithDB(t *testing.T, dbPath string, args ...string) string {
	t.Helper()
	oldOpts := opts
	opts = globalOptions{dbPath: dbPath}
	defer func() { opts = oldOpts }()

	c := newDecideCommand()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&bytes.Buffer{})
	c.SetArgs(args)
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func runSearchCommandWithDB(t *testing.T, dbPath string, args ...string) string {
	t.Helper()
	oldOpts := opts
	opts = globalOptions{dbPath: dbPath}
	defer func() { opts = oldOpts }()

	c := newSearchCommand()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&bytes.Buffer{})
	c.SetArgs(args)
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func runRelayCommand(t *testing.T, args ...string) string {
	t.Helper()
	oldOpts := opts
	opts = globalOptions{}
	defer func() { opts = oldOpts }()

	c := newRelayCommand()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&bytes.Buffer{})
	c.SetArgs(args)
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	return out.String()
}
