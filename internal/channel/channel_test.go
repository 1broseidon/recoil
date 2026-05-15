package channel

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/1broseidon/recoil/internal/store"
)

func TestMemoryArtifactEventsAreSignedAndTamperEvident(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "recoil.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	identity, err := st.GetOrCreateChannelIdentity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ch, err := st.UpsertChannelSubscription(ctx, store.UpsertChannelSubscriptionParams{
		ChannelID: "ch_test",
		Name:      "test",
		Path:      t.TempDir(),
		NodeID:    identity.NodeID,
		Agent:     "alpha",
		ScopeKind: "session",
		ScopeID:   "signed-events",
	})
	if err != nil {
		t.Fatal(err)
	}
	mem, _, err := st.AddMemory(ctx, store.AddMemoryParams{
		Role:      "decision",
		Content:   "Signed artifacts should fail verification after mutation.",
		ScopeKind: "session",
		ScopeID:   "signed-events",
		Validity:  "active",
		ClaimKey:  "channel.signing",
	})
	if err != nil {
		t.Fatal(err)
	}

	event := NewMemoryArtifactEvent(ch, identity, *mem)
	if err := SignMemoryArtifactEvent(&event, identity.PrivateKey); err != nil {
		t.Fatal(err)
	}
	if event.EventID == "" || event.Signature == "" {
		t.Fatalf("expected signed event id and signature, got %+v", event)
	}
	if err := VerifyMemoryArtifactEvent(event); err != nil {
		t.Fatal(err)
	}

	event.Content = "mutated content"
	if err := VerifyMemoryArtifactEvent(event); err == nil || !strings.Contains(err.Error(), "event id hash mismatch") {
		t.Fatalf("expected tamper verification failure, got %v", err)
	}
}

func TestChannelRosterAndArtifactReplay(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "recoil.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	channelDir := t.TempDir()
	manifest, path, err := EnsureChannel(channelDir, "demo")
	if err != nil {
		t.Fatal(err)
	}
	identity, err := st.GetOrCreateChannelIdentity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ch, err := st.UpsertChannelSubscription(ctx, store.UpsertChannelSubscriptionParams{
		ChannelID: manifest.ChannelID,
		Name:      manifest.Name,
		Path:      path,
		NodeID:    identity.NodeID,
		Agent:     "alpha",
		ScopeKind: "session",
		ScopeID:   "replay",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteRosterCard(path, BuildRosterCard(ch, identity), identity.PrivateKey); err != nil {
		t.Fatal(err)
	}
	roster, err := ReadRoster(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(roster) != 1 || roster[0].NodeID != identity.NodeID {
		t.Fatalf("expected signed roster card, got %+v", roster)
	}

	mem, _, err := st.AddMemory(ctx, store.AddMemoryParams{
		Role:      "decision",
		Content:   "Replay logs expose a durable artifact index.",
		ScopeKind: "session",
		ScopeID:   "replay",
		Validity:  "active",
		ClaimKey:  "channel.replay",
	})
	if err != nil {
		t.Fatal(err)
	}
	event := NewMemoryArtifactEvent(ch, identity, *mem)
	if err := SignMemoryArtifactEvent(&event, identity.PrivateKey); err != nil {
		t.Fatal(err)
	}
	duplicate, err := AppendEventIfMissing(path, event)
	if err != nil {
		t.Fatal(err)
	}
	if duplicate {
		t.Fatal("first append should not be a duplicate")
	}
	duplicate, err = AppendEventIfMissing(path, event)
	if err != nil {
		t.Fatal(err)
	}
	if !duplicate {
		t.Fatal("second append should be detected as a duplicate")
	}
	events, err := ReadEvents(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].ArtifactID != event.ArtifactID {
		t.Fatalf("expected one replayed event, got %+v", events)
	}
}

func TestAppendEventIfMissingIsConcurrencySafe(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "recoil.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	manifest, path, err := EnsureChannel(t.TempDir(), "concurrent")
	if err != nil {
		t.Fatal(err)
	}
	identity, err := st.GetOrCreateChannelIdentity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ch, err := st.UpsertChannelSubscription(ctx, store.UpsertChannelSubscriptionParams{
		ChannelID: manifest.ChannelID,
		Name:      manifest.Name,
		Path:      path,
		NodeID:    identity.NodeID,
		Agent:     "alpha",
		ScopeKind: "session",
		ScopeID:   "concurrent-append",
	})
	if err != nil {
		t.Fatal(err)
	}
	mem, _, err := st.AddMemory(ctx, store.AddMemoryParams{
		Role:      "decision",
		Content:   "Concurrent appends of the same artifact must collapse to one log entry.",
		ScopeKind: "session",
		ScopeID:   "concurrent-append",
		Validity:  "active",
		ClaimKey:  "channel.append-race",
	})
	if err != nil {
		t.Fatal(err)
	}
	event := NewMemoryArtifactEvent(ch, identity, *mem)
	if err := SignMemoryArtifactEvent(&event, identity.PrivateKey); err != nil {
		t.Fatal(err)
	}

	const N = 16
	results := make([]bool, N)
	errs := make([]error, N)
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		i := i
		go func() {
			defer wg.Done()
			dup, err := AppendEventIfMissing(path, event)
			results[i] = dup
			errs[i] = err
		}()
	}
	wg.Wait()

	writes := 0
	for i := 0; i < N; i++ {
		if errs[i] != nil {
			t.Fatalf("append %d: %v", i, errs[i])
		}
		if !results[i] {
			writes++
		}
	}
	if writes != 1 {
		t.Fatalf("expected exactly one non-duplicate write across %d goroutines, got %d", N, writes)
	}
	events, err := ReadEvents(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected exactly one event in log, got %d", len(events))
	}
}
