package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	channelpkg "github.com/1broseidon/recoil/internal/channel"
	"github.com/1broseidon/recoil/internal/config"
	"github.com/1broseidon/recoil/internal/store"
	"github.com/spf13/cobra"
)

const (
	channelAutoPublishOff      = "off"
	channelAutoPublishGuidance = "guidance"
	channelAutoPublishAllLocal = "all-local"

	channelJITOff     = "off"
	channelJITWake    = "wake"
	channelJITContext = "context"
	channelJITAll     = "all"
)

type channelPolicy struct {
	AutoPublish              string
	AutoPublishRoles         map[string]bool
	AutoPublishRequiresKey   bool
	AutoPublishIncludeRemote bool
	JITRefresh               string
	JITRefreshTimeout        time.Duration
	OutboxFlushTimeout       time.Duration
}

type channelAutoPublishOptions struct {
	Command   string
	Force     bool
	Disabled  bool
	Duplicate bool
}

type channelAutoPublishResult struct {
	Mode      string   `json:"mode"`
	Eligible  bool     `json:"eligible"`
	Published int      `json:"published"`
	Queued    int      `json:"queued"`
	Duplicate int      `json:"duplicate"`
	Failed    int      `json:"failed"`
	Skipped   string   `json:"skipped,omitempty"`
	Errors    []string `json:"errors,omitempty"`
}

type channelOutboxFlushResult struct {
	Published int      `json:"published"`
	Duplicate int      `json:"duplicate"`
	Failed    int      `json:"failed"`
	Pending   int      `json:"pending"`
	Errors    []string `json:"errors,omitempty"`
}

type channelFreshnessResult struct {
	Enabled bool                     `json:"enabled"`
	Reason  string                   `json:"reason"`
	Outbox  channelOutboxFlushResult `json:"outbox"`
	Refresh []channelRefreshResult   `json:"refresh,omitempty"`
}

type channelOutboxOptions struct {
	channel string
	all     bool
	limit   int
	timeout time.Duration
}

func effectiveChannelPolicy(settings config.Settings) channelPolicy {
	autoPublish := strings.ToLower(strings.TrimSpace(settingsString(settings, "channel.auto_publish", channelAutoPublishOff)))
	switch autoPublish {
	case channelAutoPublishGuidance, channelAutoPublishAllLocal:
	default:
		autoPublish = channelAutoPublishOff
	}
	jit := strings.ToLower(strings.TrimSpace(settingsString(settings, "channel.jit_refresh", channelJITContext)))
	switch jit {
	case channelJITWake, channelJITContext, channelJITAll:
	default:
		jit = channelJITOff
	}
	return channelPolicy{
		AutoPublish:              autoPublish,
		AutoPublishRoles:         roleSetFromCSV(settingsString(settings, "channel.auto_publish_roles", "decision,adr,constraint,preference,rule,handoff")),
		AutoPublishRequiresKey:   settings.Bool("channel.auto_publish_requires_claim_key", true),
		AutoPublishIncludeRemote: settings.Bool("channel.auto_publish_include_remote", false),
		JITRefresh:               jit,
		JITRefreshTimeout:        settingsDuration(settings, "channel.jit_refresh_timeout", 2*time.Second),
		OutboxFlushTimeout:       settingsDuration(settings, "channel.outbox_flush_timeout", 2*time.Second),
	}
}

func settingsString(settings config.Settings, key, def string) string {
	if value, ok := settings.Get(key); ok && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return def
}

func settingsDuration(settings config.Settings, key string, def time.Duration) time.Duration {
	raw := settingsString(settings, key, "")
	if raw == "" {
		return def
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil || parsed <= 0 {
		return def
	}
	return parsed
}

func roleSetFromCSV(raw string) map[string]bool {
	roles := map[string]bool{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.ToLower(strings.TrimSpace(part))
		if part != "" {
			roles[part] = true
		}
	}
	return roles
}

func autoPublishMemory(ctx context.Context, st *store.Store, mem *store.Memory, opts channelAutoPublishOptions) channelAutoPublishResult {
	result := channelAutoPublishResult{Mode: channelAutoPublishOff}
	if mem == nil {
		result.Skipped = "missing_memory"
		return result
	}
	policy := loadChannelPolicyOrDefault()
	result.Mode = policy.AutoPublish
	if opts.Disabled {
		result.Skipped = "disabled_by_flag"
		return result
	}
	if !opts.Force && !channelMemoryAutoPublishEligible(*mem, policy) {
		result.Skipped = "policy"
		return result
	}
	result.Eligible = true
	channels, err := matchingPublishChannels(ctx, st, *mem)
	if err != nil {
		result.Failed++
		result.Errors = append(result.Errors, err.Error())
		return result
	}
	if len(channels) == 0 {
		result.Skipped = "no_matching_channels"
		return result
	}
	reason := "auto_publish"
	if opts.Command != "" {
		reason += ":" + opts.Command
	}
	for _, ch := range channels {
		if err := st.EnqueueChannelOutbox(ctx, mem.ID, ch.ChannelID, reason); err != nil {
			result.Failed++
			result.Errors = append(result.Errors, err.Error())
			continue
		}
		result.Queued++
	}
	if result.Queued == 0 {
		return result
	}
	identity, err := st.GetOrCreateChannelIdentity(ctx)
	if err != nil {
		result.Failed++
		result.Errors = append(result.Errors, err.Error())
		return result
	}
	flushCtx, cancel := context.WithTimeout(ctx, policy.OutboxFlushTimeout)
	defer cancel()
	flush := flushChannelOutbox(flushCtx, st, identity, channels, 100)
	result.Published += flush.Published
	result.Duplicate += flush.Duplicate
	result.Failed += flush.Failed
	result.Errors = append(result.Errors, flush.Errors...)
	return result
}

func loadChannelPolicyOrDefault() channelPolicy {
	_, settings, err := loadProjectSettings()
	if err != nil {
		return effectiveChannelPolicy(config.Settings{Values: map[string]string{}})
	}
	return effectiveChannelPolicy(settings)
}

func channelMemoryAutoPublishEligible(mem store.Memory, policy channelPolicy) bool {
	if policy.AutoPublish == channelAutoPublishOff {
		return false
	}
	if mem.TombstonedAt != "" || isHistoricalMemory(mem) {
		return false
	}
	if !policy.AutoPublishIncludeRemote && strings.EqualFold(mem.SourceKind, "remote_artifact") {
		return false
	}
	if policy.AutoPublishRequiresKey && strings.TrimSpace(mem.ClaimKey) == "" {
		return false
	}
	switch policy.AutoPublish {
	case channelAutoPublishGuidance:
		return policy.AutoPublishRoles[strings.ToLower(strings.TrimSpace(mem.Role))]
	case channelAutoPublishAllLocal:
		return true
	default:
		return false
	}
}

func matchingPublishChannels(ctx context.Context, st *store.Store, mem store.Memory) ([]store.ChannelSubscription, error) {
	channels, err := st.ChannelSubscriptions(ctx)
	if err != nil {
		return nil, err
	}
	var out []store.ChannelSubscription
	for _, ch := range channels {
		if ch.ScopeKind == mem.ScopeKind && ch.ScopeID == mem.ScopeID {
			out = append(out, ch)
		}
	}
	return out, nil
}

func prepareChannelForPublish(ctx context.Context, ch store.ChannelSubscription, identity store.ChannelIdentity) error {
	if _, err := loadJoinedChannelManifest(ctx, ch); err != nil {
		return err
	}
	card := channelpkg.BuildRosterCard(ch, identity)
	if isRelayTarget(ch.Path) {
		if err := channelpkg.SignRosterCard(&card, identity.PrivateKey); err != nil {
			return err
		}
		return relayUpdateRoster(ctx, ch, identity, card)
	}
	return channelpkg.WriteRosterCard(ch.Path, card, identity.PrivateKey)
}

func publishMemoryToChannel(ctx context.Context, ch store.ChannelSubscription, identity store.ChannelIdentity, mem store.Memory, dryRun bool) (channelpkg.MemoryArtifactEvent, bool, error) {
	event := channelpkg.NewMemoryArtifactEvent(ch, identity, mem)
	if err := channelpkg.SignMemoryArtifactEvent(&event, identity.PrivateKey); err != nil {
		return event, false, err
	}
	if dryRun {
		return event, false, nil
	}
	duplicate, err := appendChannelEvent(ctx, ch, identity, event)
	return event, duplicate, err
}

func flushChannelOutbox(ctx context.Context, st *store.Store, identity store.ChannelIdentity, channels []store.ChannelSubscription, limit int) channelOutboxFlushResult {
	result := channelOutboxFlushResult{}
	byID := map[string]store.ChannelSubscription{}
	for _, ch := range channels {
		byID[ch.ChannelID] = ch
	}
	prepared := map[string]error{}
	for _, ch := range channels {
		entries, err := st.PendingChannelOutbox(ctx, ch.ChannelID, limit)
		if err != nil {
			result.Failed++
			result.Errors = append(result.Errors, err.Error())
			continue
		}
		for _, entry := range entries {
			ch, ok := byID[entry.ChannelID]
			if !ok {
				continue
			}
			if _, seen := prepared[ch.ChannelID]; !seen {
				prepared[ch.ChannelID] = prepareChannelForPublish(ctx, ch, identity)
			}
			if err := prepared[ch.ChannelID]; err != nil {
				result.Failed++
				result.Pending++
				result.Errors = append(result.Errors, err.Error())
				_ = st.RecordChannelOutboxFailure(ctx, entry.MemoryID, entry.ChannelID, err.Error())
				continue
			}
			mem, err := st.GetMemory(ctx, entry.MemoryID)
			if err != nil {
				result.Failed++
				result.Pending++
				result.Errors = append(result.Errors, err.Error())
				_ = st.RecordChannelOutboxFailure(ctx, entry.MemoryID, entry.ChannelID, err.Error())
				continue
			}
			_, duplicate, err := publishMemoryToChannel(ctx, ch, identity, *mem, false)
			if err != nil {
				result.Failed++
				result.Pending++
				result.Errors = append(result.Errors, err.Error())
				_ = st.RecordChannelOutboxFailure(ctx, entry.MemoryID, entry.ChannelID, err.Error())
				continue
			}
			if duplicate {
				result.Duplicate++
			} else {
				result.Published++
			}
			if err := st.MarkChannelOutboxPublished(ctx, entry.MemoryID, entry.ChannelID); err != nil {
				result.Errors = append(result.Errors, err.Error())
			}
		}
	}
	return result
}

func ensureFreshContext(ctx context.Context, st *store.Store, reason string) channelFreshnessResult {
	policy := loadChannelPolicyOrDefault()
	result := channelFreshnessResult{Enabled: shouldJITRefresh(policy.JITRefresh, reason), Reason: reason}
	if !result.Enabled {
		return result
	}
	channels, err := st.ChannelSubscriptions(ctx)
	if err != nil || len(channels) == 0 {
		return result
	}
	refreshCtx, cancel := context.WithTimeout(ctx, policy.JITRefreshTimeout)
	defer cancel()
	identity, err := st.GetOrCreateChannelIdentity(refreshCtx)
	if err != nil {
		result.Refresh = []channelRefreshResult{{Error: err.Error()}}
		return result
	}
	result.Outbox = flushChannelOutbox(refreshCtx, st, identity, channels, 100)
	result.Refresh = refreshChannels(refreshCtx, st, identity, channels)
	return result
}

func shouldJITRefresh(mode, reason string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case channelJITAll:
		return true
	case channelJITWake:
		return reason == "wake"
	case channelJITContext:
		switch reason {
		case "wake", "search", "check", "handoff":
			return true
		default:
			return false
		}
	default:
		return false
	}
}

func channelFreshnessImported(result channelFreshnessResult) int {
	return channelRefreshImported(result.Refresh)
}

func channelFreshnessErrors(result channelFreshnessResult) int {
	return channelRefreshErrors(result.Refresh) + len(result.Outbox.Errors)
}

func channelOutboxFrontmatter(prefix string, result channelOutboxFlushResult) []kv {
	return []kv{
		{k: prefix + "outbox_published", v: fmt.Sprintf("%d", result.Published)},
		{k: prefix + "outbox_duplicate", v: fmt.Sprintf("%d", result.Duplicate)},
		{k: prefix + "outbox_failed", v: fmt.Sprintf("%d", result.Failed)},
		{k: prefix + "outbox_pending", v: fmt.Sprintf("%d", result.Pending)},
	}
}

func channelFriendlyFrontmatter(freshness channelFreshnessResult) []kv {
	return []kv{
		{k: "peer_memory_received", v: fmt.Sprintf("%d", channelFreshnessImported(freshness))},
		{k: "memory_shared", v: fmt.Sprintf("%d", freshness.Outbox.Published)},
		{k: "pending_share", v: fmt.Sprintf("%d", freshness.Outbox.Pending)},
	}
}

func autoPublishFrontmatter(result channelAutoPublishResult) []kv {
	return []kv{
		{k: "publish_mode", v: result.Mode},
		{k: "memory_shared", v: fmt.Sprintf("%d", result.Published)},
		{k: "publish_published", v: fmt.Sprintf("%d", result.Published)},
		{k: "publish_queued", v: fmt.Sprintf("%d", result.Queued)},
		{k: "publish_duplicate", v: fmt.Sprintf("%d", result.Duplicate)},
		{k: "publish_failed", v: fmt.Sprintf("%d", result.Failed)},
		{k: "publish_skipped", v: result.Skipped},
	}
}

func newChannelOutboxCommand() *cobra.Command {
	var outboxOpts channelOutboxOptions
	c := &cobra.Command{
		Use:   "outbox",
		Short: "Inspect pending automatic channel publishes",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			st, _, err := openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			ctx := context.Background()
			channelID := ""
			if strings.TrimSpace(outboxOpts.channel) != "" {
				ch, err := st.ResolveChannelSubscription(ctx, outboxOpts.channel)
				if err != nil {
					return err
				}
				channelID = ch.ChannelID
			}
			entries, err := st.ChannelOutboxEntries(ctx, channelID, outboxOpts.all, outboxOpts.limit)
			if err != nil {
				return err
			}
			if opts.json {
				return writeJSON(cmd.OutOrStdout(), "channel_outbox_result", entries)
			}
			return writeChannelOutbox(cmd, entries)
		},
	}
	c.Flags().StringVar(&outboxOpts.channel, "channel", "", "channel id, name, or path")
	c.Flags().BoolVar(&outboxOpts.all, "all", false, "include published outbox entries")
	c.Flags().IntVar(&outboxOpts.limit, "limit", 100, "maximum outbox entries to show")
	c.AddCommand(newChannelOutboxFlushCommand())
	return c
}

func newChannelOutboxFlushCommand() *cobra.Command {
	var outboxOpts channelOutboxOptions
	c := &cobra.Command{
		Use:   "flush",
		Short: "Flush pending automatic channel publishes",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			st, _, err := openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			ctx := context.Background()
			if outboxOpts.timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, outboxOpts.timeout)
				defer cancel()
			}
			identity, err := st.GetOrCreateChannelIdentity(ctx)
			if err != nil {
				return err
			}
			channels, err := resolveChannelSubscriptions(ctx, st, outboxOpts.channel, true)
			if err != nil {
				return err
			}
			result := flushChannelOutbox(ctx, st, identity, channels, outboxOpts.limit)
			if opts.json {
				return writeJSON(cmd.OutOrStdout(), "channel_outbox_flush_result", result)
			}
			return frontmatter(cmd.OutOrStdout(), channelOutboxFrontmatter("", result), strings.Join(result.Errors, "\n"))
		},
	}
	c.Flags().StringVar(&outboxOpts.channel, "channel", "", "channel id, name, or path")
	c.Flags().IntVar(&outboxOpts.limit, "limit", 100, "maximum pending entries to flush")
	c.Flags().DurationVar(&outboxOpts.timeout, "timeout", 5*time.Second, "maximum time to spend flushing the outbox")
	return c
}

func writeChannelOutbox(cmd *cobra.Command, entries []store.ChannelOutboxEntry) error {
	var b strings.Builder
	pending := 0
	published := 0
	for _, entry := range entries {
		if entry.Status == "published" {
			published++
		} else {
			pending++
		}
		fmt.Fprintf(&b, "- %s channel=%s status=%s attempts=%d reason=%s",
			entry.MemoryID, entry.ChannelID, entry.Status, entry.Attempts, entry.Reason)
		if entry.LastError != "" {
			fmt.Fprintf(&b, " last_error=%q", entry.LastError)
		}
		b.WriteByte('\n')
	}
	return frontmatter(cmd.OutOrStdout(), []kv{
		{k: "outbox_count", v: fmt.Sprintf("%d", len(entries))},
		{k: "pending", v: fmt.Sprintf("%d", pending)},
		{k: "published", v: fmt.Sprintf("%d", published)},
	}, b.String())
}
