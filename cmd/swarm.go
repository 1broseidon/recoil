package cmd

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/1broseidon/recoil/internal/config"
	"github.com/1broseidon/recoil/internal/scope"
	"github.com/1broseidon/recoil/internal/store"
	"github.com/spf13/cobra"
)

type swarmOptions struct {
	refresh bool
}

type swarmResult struct {
	Tree               string                      `json:"tree"`
	Scope              string                      `json:"scope,omitempty"`
	ScopeID            string                      `json:"scope_id,omitempty"`
	Posture            string                      `json:"posture"`
	Sharing            string                      `json:"sharing"`
	Peers              []string                    `json:"peers"`
	PeerCount          int                         `json:"peer_count"`
	Memory             int                         `json:"memory"`
	PeerMemory         int                         `json:"peer_memory"`
	Evidence           string                      `json:"evidence"`
	EvidenceCount      int                         `json:"evidence_count"`
	Review             string                      `json:"review"`
	PendingShare       int                         `json:"pending_share"`
	PeerMemoryReceived int                         `json:"peer_memory_received"`
	MemoryShared       int                         `json:"memory_shared"`
	Channels           []store.ChannelSubscription `json:"channels,omitempty"`
	Outbox             channelOutboxFlushResult    `json:"outbox,omitempty"`
	Refresh            []channelRefreshResult      `json:"refresh,omitempty"`
	Errors             []string                    `json:"errors,omitempty"`
}

func newSwarmCommand() *cobra.Command {
	var swarmOpts swarmOptions
	c := &cobra.Command{
		Use:   "swarm",
		Short: "Show this workspace memory tree and sharing health",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			st, _, err := openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			ctx := context.Background()
			result, err := buildSwarmResult(ctx, st, swarmOpts.refresh)
			if err != nil {
				return err
			}
			if opts.json {
				return writeJSON(cmd.OutOrStdout(), "swarm_result", result)
			}
			return frontmatter(cmd.OutOrStdout(), []kv{
				{k: "tree", v: result.Tree},
				{k: "posture", v: result.Posture},
				{k: "sharing", v: result.Sharing},
				{k: "peers", v: fmt.Sprintf("%d", result.PeerCount)},
				{k: "memory", v: fmt.Sprintf("%d", result.Memory)},
				{k: "peer_memory", v: fmt.Sprintf("%d", result.PeerMemory)},
				{k: "evidence", v: result.Evidence},
				{k: "review", v: result.Review},
				{k: "pending_share", v: fmt.Sprintf("%d", result.PendingShare)},
				{k: "peer_memory_received", v: fmt.Sprintf("%d", result.PeerMemoryReceived)},
				{k: "memory_shared", v: fmt.Sprintf("%d", result.MemoryShared)},
			}, renderSwarmResult(result))
		},
	}
	c.Flags().BoolVar(&swarmOpts.refresh, "refresh", false, "send pending shares and receive peer memory before showing the card")
	return c
}

func buildSwarmResult(ctx context.Context, st *store.Store, refresh bool) (swarmResult, error) {
	_, settings, err := loadProjectSettings()
	if err != nil {
		return swarmResult{}, err
	}
	tree, scopeKind, scopeID := currentTree()
	channels, err := st.ChannelSubscriptions(ctx)
	if err != nil {
		return swarmResult{}, err
	}
	result := swarmResult{
		Tree:     tree,
		Scope:    scopeKind,
		ScopeID:  scopeID,
		Posture:  inferSwarmPosture(settings, channels),
		Sharing:  "off",
		Evidence: swarmEvidenceState(settings),
		Review:   "0",
		Channels: channels,
	}
	if len(channels) > 0 {
		result.Sharing = "connected"
	}
	if refresh && len(channels) > 0 {
		policy := effectiveChannelPolicy(settings)
		timeout := policy.JITRefreshTimeout
		if timeout <= 0 {
			timeout = 5 * time.Second
		}
		refreshCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		identity, err := st.GetOrCreateChannelIdentity(refreshCtx)
		if err != nil {
			result.Sharing = "degraded"
			result.Errors = append(result.Errors, err.Error())
		} else {
			result.Outbox = flushChannelOutbox(refreshCtx, st, identity, channels, 100)
			result.Refresh = refreshChannels(refreshCtx, st, identity, channels)
			result.PeerMemoryReceived = channelRefreshImported(result.Refresh)
			result.MemoryShared = result.Outbox.Published
			result.Errors = append(result.Errors, result.Outbox.Errors...)
			for _, item := range result.Refresh {
				if item.Error != "" {
					result.Errors = append(result.Errors, item.Error)
				}
			}
			if len(result.Errors) > 0 {
				result.Sharing = "degraded"
			}
		}
	}
	counts, err := st.Counts(ctx)
	if err != nil {
		return swarmResult{}, err
	}
	peerMemory, err := st.ActiveSourceKindCount(ctx, "remote_artifact")
	if err != nil {
		return swarmResult{}, err
	}
	evidenceCount, err := st.ActiveSourceKindCount(ctx, "session_evidence")
	if err != nil {
		return swarmResult{}, err
	}
	result.PeerMemory = peerMemory
	result.EvidenceCount = evidenceCount
	result.Memory = counts.Active - peerMemory - evidenceCount
	if result.Memory < 0 {
		result.Memory = 0
	}
	result.Peers = swarmPeerNames(ctx, st, channels)
	result.PeerCount = len(result.Peers)
	pending, err := st.ChannelOutboxEntries(ctx, "", false, 1000)
	if err != nil {
		return swarmResult{}, err
	}
	result.PendingShare = len(pending)
	if refresh && result.Outbox.Pending > result.PendingShare {
		result.PendingShare = result.Outbox.Pending
	}
	return result, nil
}

func currentTree() (tree, scopeKind, scopeID string) {
	sc, err := scope.ProjectScope(".")
	if err == nil && sc.Initialized {
		return "project", sc.Kind, sc.ID
	}
	return "user", "user", ""
}

func inferSwarmPosture(settings config.Settings, channels []store.ChannelSubscription) string {
	if len(channels) == 0 {
		return setupPostureStandalone
	}
	policy := effectiveChannelPolicy(settings)
	switch {
	case policy.AutoPublish == channelAutoPublishGuidance && policy.JITRefresh == channelJITContext:
		return setupPostureCollaborative
	case policy.AutoPublish == channelAutoPublishOff && policy.JITRefresh == channelJITContext:
		return setupPostureManualShare
	default:
		return "custom"
	}
}

func swarmEvidenceState(settings config.Settings) string {
	if settings.Bool("session-evidence.enabled", false) {
		return "on"
	}
	return "off"
}

func swarmPeerNames(ctx context.Context, st *store.Store, channels []store.ChannelSubscription) []string {
	seen := map[string]bool{}
	var out []string
	for _, ch := range channels {
		peers, err := st.PresentChannelPeers(ctx, ch.ChannelID)
		if err != nil {
			continue
		}
		for _, peer := range peers {
			name := firstNonEmpty(peer.Agent, peer.NodeID)
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func renderSwarmResult(result swarmResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "memory tree: %s\n", result.Tree)
	fmt.Fprintf(&b, "posture: %s\n", result.Posture)
	fmt.Fprintf(&b, "sharing: %s\n", result.Sharing)
	if len(result.Peers) == 0 {
		fmt.Fprintf(&b, "peers: %d\n", result.PeerCount)
	} else {
		fmt.Fprintf(&b, "peers: %d (%s)\n", result.PeerCount, strings.Join(result.Peers, ", "))
	}
	fmt.Fprintf(&b, "memory: %d\n", result.Memory)
	fmt.Fprintf(&b, "peer_memory: %d\n", result.PeerMemory)
	if result.Evidence == "on" {
		fmt.Fprintf(&b, "evidence: on (redacted)\n")
	} else {
		fmt.Fprintf(&b, "evidence: off\n")
	}
	fmt.Fprintf(&b, "review: %s\n", result.Review)
	fmt.Fprintf(&b, "pending_share: %d\n", result.PendingShare)
	if result.PeerMemoryReceived > 0 || result.MemoryShared > 0 {
		fmt.Fprintf(&b, "peer_memory_received: %d\n", result.PeerMemoryReceived)
		fmt.Fprintf(&b, "memory_shared: %d\n", result.MemoryShared)
	}
	if len(result.Errors) > 0 {
		fmt.Fprintf(&b, "sharing_note: %s\n", strings.Join(result.Errors, "; "))
	}
	return b.String()
}

func swarmPresenceLine(ctx context.Context, st *store.Store, freshness channelFreshnessResult) string {
	channels, err := st.ChannelSubscriptions(ctx)
	if err != nil || len(channels) == 0 {
		return ""
	}
	peerCount := len(swarmPeerNames(ctx, st, channels))
	for _, refresh := range freshness.Refresh {
		if refresh.RosterCount > 0 && refresh.RosterCount-1 > peerCount {
			peerCount = refresh.RosterCount - 1
		}
	}
	pendingShare := freshness.Outbox.Pending
	if pendingShare == 0 {
		if pending, err := st.ChannelOutboxEntries(ctx, "", false, 1000); err == nil {
			pendingShare = len(pending)
		}
	}
	return fmt.Sprintf("swarm: %d peers; %d peer memories received; %d pending share",
		peerCount,
		channelFreshnessImported(freshness),
		pendingShare,
	)
}
