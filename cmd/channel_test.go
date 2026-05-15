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
