package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	channelpkg "github.com/1broseidon/recoil/internal/channel"
	"github.com/1broseidon/recoil/internal/store"
	"github.com/spf13/cobra"
)

type channelJoinOptions struct {
	scope scopeOptions
	name  string
	agent string
}

type channelPublishOptions struct {
	channel       string
	limit         int
	roles         []string
	all           bool
	includeRemote bool
}

type channelSyncOptions struct {
	channel string
}

type channelRosterOptions struct {
	channel string
}

type channelStatusResult struct {
	Channels []store.ChannelSubscription `json:"channels"`
}

type channelJoinResult struct {
	Channel       store.ChannelSubscription    `json:"channel"`
	Manifest      channelpkg.ChannelManifest   `json:"manifest"`
	Roster        []channelpkg.RosterCard      `json:"roster"`
	ArtifactIndex []channelpkg.ArtifactSummary `json:"artifact_index"`
}

type channelPublishResult struct {
	ChannelID string                           `json:"channel_id"`
	Published int                              `json:"published"`
	Skipped   int                              `json:"skipped"`
	Events    []channelpkg.MemoryArtifactEvent `json:"events"`
}

type channelSyncImport struct {
	EventID    string `json:"event_id"`
	ArtifactID string `json:"artifact_id"`
	MemoryID   string `json:"memory_id,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

type channelSyncResult struct {
	ChannelID        string              `json:"channel_id"`
	Imported         int                 `json:"imported"`
	SkippedSelf      int                 `json:"skipped_self"`
	SkippedDuplicate int                 `json:"skipped_duplicate"`
	SkippedInvalid   int                 `json:"skipped_invalid"`
	Imports          []channelSyncImport `json:"imports"`
}

func newChannelCommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "channel",
		Short: "Join and exchange scoped memory artifacts over durable local channels",
		Long: `Join and exchange scoped memory artifacts over durable pub/sub-style channels.

A channel is a local-first replay log: peers publish signed memory
artifacts into the channel, other joined Recoil databases sync those artifacts
as remote evidence, and each node keeps its own local memory projection.`,
	}
	c.AddCommand(newChannelJoinCommand())
	c.AddCommand(newChannelStatusCommand())
	c.AddCommand(newChannelRosterCommand())
	c.AddCommand(newChannelPublishCommand())
	c.AddCommand(newChannelSyncCommand())
	return c
}

func newChannelJoinCommand() *cobra.Command {
	var joinOpts channelJoinOptions
	c := &cobra.Command{
		Use:   "join <channel-dir-or-invite-url>",
		Short: "Join or create a memory channel",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sc, err := resolveScope(cmd, joinOpts.scope)
			if err != nil {
				return err
			}
			st, _, err := openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			ctx := context.Background()
			identity, err := st.GetOrCreateChannelIdentity(ctx)
			if err != nil {
				return err
			}
			if isRelayTarget(args[0]) {
				result, err := joinRelayChannel(ctx, st, identity, sc, joinOpts.agent, args[0])
				if err != nil {
					return err
				}
				if opts.json {
					return writeJSON(cmd.OutOrStdout(), "channel_join_result", result)
				}
				return writeChannelJoin(cmd, result)
			}
			manifest, channelPath, err := channelpkg.EnsureChannel(args[0], joinOpts.name)
			if err != nil {
				return err
			}
			ch, err := st.UpsertChannelSubscription(ctx, store.UpsertChannelSubscriptionParams{
				ChannelID: manifest.ChannelID,
				Name:      manifest.Name,
				Path:      channelPath,
				NodeID:    identity.NodeID,
				Agent:     firstNonEmpty(joinOpts.agent, "recoil"),
				ScopeKind: sc.Kind,
				ScopeID:   sc.ID,
				ProjectID: sc.ProjectID,
				SessionID: sc.SessionID,
			})
			if err != nil {
				return err
			}
			if err := channelpkg.WriteRosterCard(ch.Path, channelpkg.BuildRosterCard(ch, identity), identity.PrivateKey); err != nil {
				return err
			}
			roster, err := channelpkg.ReadRoster(ch.Path)
			if err != nil {
				return err
			}
			events, err := channelpkg.ReadEvents(ch.Path)
			if err != nil {
				return err
			}
			result := channelJoinResult{
				Channel:       ch,
				Manifest:      manifest,
				Roster:        roster,
				ArtifactIndex: channelpkg.ArtifactSummaries(events),
			}
			if opts.json {
				return writeJSON(cmd.OutOrStdout(), "channel_join_result", result)
			}
			return writeChannelJoin(cmd, result)
		},
	}
	addScopeFlags(c, &joinOpts.scope)
	c.Flags().StringVar(&joinOpts.name, "name", "", "human-readable channel name")
	c.Flags().StringVar(&joinOpts.agent, "agent", "", "agent name to publish in the channel roster")
	return c
}

func newChannelStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "List joined channels",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			st, _, err := openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			channels, err := st.ChannelSubscriptions(context.Background())
			if err != nil {
				return err
			}
			result := channelStatusResult{Channels: channels}
			if opts.json {
				return writeJSON(cmd.OutOrStdout(), "channel_status_result", result)
			}
			var b strings.Builder
			for _, ch := range channels {
				fmt.Fprintf(&b, "## %s\n", firstNonEmpty(ch.Name, ch.ChannelID))
				fmt.Fprintf(&b, "channel_id: %s\npath: %s\nagent: %s\nscope: %s\nscope_id: %s\nnode_id: %s\n\n",
					ch.ChannelID, ch.Path, ch.Agent, ch.ScopeKind, ch.ScopeID, ch.NodeID)
			}
			return frontmatter(cmd.OutOrStdout(), []kv{{k: "channel_count", v: fmt.Sprintf("%d", len(channels))}}, b.String())
		},
	}
}

func newChannelRosterCommand() *cobra.Command {
	var rosterOpts channelRosterOptions
	c := &cobra.Command{
		Use:   "roster",
		Short: "Show joined peers and artifact index for channels",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			st, _, err := openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			ctx := context.Background()
			identity, err := st.GetOrCreateChannelIdentity(ctx)
			if err != nil {
				return err
			}
			channels, err := resolveChannelSubscriptions(ctx, st, rosterOpts.channel, true)
			if err != nil {
				return err
			}
			results := make([]channelJoinResult, 0, len(channels))
			for _, ch := range channels {
				manifest, err := loadJoinedChannelManifest(ctx, ch)
				if err != nil {
					return err
				}
				roster, err := readChannelRoster(ctx, ch, identity)
				if err != nil {
					return err
				}
				events, err := readChannelEvents(ctx, ch, identity)
				if err != nil {
					return err
				}
				results = append(results, channelJoinResult{
					Channel:       ch,
					Manifest:      manifest,
					Roster:        roster,
					ArtifactIndex: channelpkg.ArtifactSummaries(events),
				})
			}
			if opts.json {
				return writeJSON(cmd.OutOrStdout(), "channel_roster_result", results)
			}
			return writeChannelRoster(cmd, results)
		},
	}
	c.Flags().StringVar(&rosterOpts.channel, "channel", "", "channel id, name, or path")
	return c
}

func newChannelPublishCommand() *cobra.Command {
	var publishOpts channelPublishOptions
	c := &cobra.Command{
		Use:   "publish",
		Short: "Publish local current memories as signed channel artifacts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			st, _, err := openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			ctx := context.Background()
			identity, err := st.GetOrCreateChannelIdentity(ctx)
			if err != nil {
				return err
			}
			ch, err := st.ResolveChannelSubscription(ctx, publishOpts.channel)
			if err != nil {
				return err
			}
			if _, err := loadJoinedChannelManifest(ctx, ch); err != nil {
				return err
			}
			card := channelpkg.BuildRosterCard(ch, identity)
			if isRelayTarget(ch.Path) {
				if err := channelpkg.SignRosterCard(&card, identity.PrivateKey); err != nil {
					return err
				}
				if err := relayUpdateRoster(ctx, ch, identity, card); err != nil {
					return err
				}
			} else if err := channelpkg.WriteRosterCard(ch.Path, card, identity.PrivateKey); err != nil {
				return err
			}
			memories, err := st.List(ctx, store.ListParams{
				ScopeKind: ch.ScopeKind,
				ScopeID:   ch.ScopeID,
				Limit:     publishOpts.limit,
				Lifecycle: store.LifecycleCurrent,
			})
			if err != nil {
				return err
			}
			result := channelPublishResult{ChannelID: ch.ChannelID}
			roleSet := publishRoleSet(publishOpts.roles, publishOpts.all)
			for _, mem := range memories {
				if !publishOpts.all && !roleSet[strings.ToLower(strings.TrimSpace(mem.Role))] {
					result.Skipped++
					continue
				}
				if !publishOpts.includeRemote && strings.EqualFold(mem.SourceKind, "remote_artifact") {
					result.Skipped++
					continue
				}
				event := channelpkg.NewMemoryArtifactEvent(ch, identity, mem)
				if err := channelpkg.SignMemoryArtifactEvent(&event, identity.PrivateKey); err != nil {
					return err
				}
				duplicate, err := appendChannelEvent(ctx, ch, identity, event)
				if err != nil {
					return err
				}
				if duplicate {
					result.Skipped++
					continue
				}
				result.Published++
				result.Events = append(result.Events, event)
			}
			if opts.json {
				return writeJSON(cmd.OutOrStdout(), "channel_publish_result", result)
			}
			return frontmatter(cmd.OutOrStdout(), []kv{
				{k: "channel_id", v: result.ChannelID},
				{k: "published", v: fmt.Sprintf("%d", result.Published)},
				{k: "skipped", v: fmt.Sprintf("%d", result.Skipped)},
			}, channelPublishedBlocks(result.Events))
		},
	}
	c.Flags().StringVar(&publishOpts.channel, "channel", "", "channel id, name, or path")
	c.Flags().IntVar(&publishOpts.limit, "limit", 100, "maximum local memories to consider")
	c.Flags().StringSliceVar(&publishOpts.roles, "role", nil, "memory role to publish; repeat or comma-separate")
	c.Flags().BoolVar(&publishOpts.all, "all", false, "publish all current local memories in the joined scope")
	c.Flags().BoolVar(&publishOpts.includeRemote, "include-remote", false, "allow republishing imported remote artifacts")
	return c
}

func newChannelSyncCommand() *cobra.Command {
	var syncOpts channelSyncOptions
	c := &cobra.Command{
		Use:   "sync",
		Short: "Replay channel artifacts into the local Recoil database as remote evidence",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			st, _, err := openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			ctx := context.Background()
			identity, err := st.GetOrCreateChannelIdentity(ctx)
			if err != nil {
				return err
			}
			channels, err := resolveChannelSubscriptions(ctx, st, syncOpts.channel, true)
			if err != nil {
				return err
			}
			results := make([]channelSyncResult, 0, len(channels))
			for _, ch := range channels {
				if _, err := loadJoinedChannelManifest(ctx, ch); err != nil {
					return err
				}
				result, err := syncChannel(ctx, st, identity, ch)
				if err != nil {
					return err
				}
				results = append(results, result)
			}
			if opts.json {
				return writeJSON(cmd.OutOrStdout(), "channel_sync_result", results)
			}
			return writeChannelSync(cmd, results)
		},
	}
	c.Flags().StringVar(&syncOpts.channel, "channel", "", "channel id, name, or path")
	return c
}

func resolveChannelSubscriptions(ctx context.Context, st *store.Store, selector string, allWhenEmpty bool) ([]store.ChannelSubscription, error) {
	if strings.TrimSpace(selector) != "" || !allWhenEmpty {
		ch, err := st.ResolveChannelSubscription(ctx, selector)
		if err != nil {
			return nil, err
		}
		return []store.ChannelSubscription{ch}, nil
	}
	channels, err := st.ChannelSubscriptions(ctx)
	if err != nil {
		return nil, err
	}
	if len(channels) == 0 {
		return nil, fmt.Errorf("no channels joined")
	}
	return channels, nil
}

func loadJoinedChannelManifest(ctx context.Context, ch store.ChannelSubscription) (channelpkg.ChannelManifest, error) {
	if isRelayTarget(ch.Path) {
		return relayLoadManifest(ctx, ch)
	}
	manifest, err := channelpkg.LoadManifest(ch.Path)
	if err != nil {
		return manifest, err
	}
	if manifest.ChannelID != ch.ChannelID {
		return manifest, fmt.Errorf("joined channel %s points at manifest %s", ch.ChannelID, manifest.ChannelID)
	}
	return manifest, nil
}

func syncChannel(ctx context.Context, st *store.Store, identity store.ChannelIdentity, ch store.ChannelSubscription) (channelSyncResult, error) {
	events, err := readChannelEvents(ctx, ch, identity)
	if err != nil {
		return channelSyncResult{}, err
	}
	result := channelSyncResult{ChannelID: ch.ChannelID}
	for _, event := range events {
		if event.ChannelID != ch.ChannelID {
			result.SkippedInvalid++
			continue
		}
		if err := channelpkg.VerifyMemoryArtifactEvent(event); err != nil {
			result.SkippedInvalid++
			continue
		}
		if event.Publisher.NodeID == identity.NodeID {
			result.SkippedSelf++
			continue
		}
		seen, err := st.HasChannelImport(ctx, event.EventID)
		if err != nil {
			return result, err
		}
		if seen {
			result.SkippedDuplicate++
			continue
		}
		metadata, err := remoteArtifactMetadata(event)
		if err != nil {
			return result, err
		}
		mem, duplicate, err := st.AddMemory(ctx, store.AddMemoryParams{
			Role:         event.Role,
			Content:      event.Content,
			SourceKind:   "remote_artifact",
			SourceAgent:  event.Publisher.Agent,
			SourcePath:   channelpkg.SourcePathForEvent(event),
			SourceRef:    event.EventID,
			ScopeKind:    ch.ScopeKind,
			ScopeID:      ch.ScopeID,
			ProjectID:    ch.ProjectID,
			SessionID:    ch.SessionID,
			MetadataJSON: metadata,
			Validity:     event.Validity,
			ClaimKey:     event.ClaimKey,
			CreatedAt:    event.PublishedAt,
		})
		if err != nil {
			return result, err
		}
		reason := ""
		if duplicate {
			reason = "duplicate_memory"
			result.SkippedDuplicate++
		} else {
			result.Imported++
		}
		if err := st.RecordChannelImport(ctx, store.ChannelImportParams{
			EventID:       event.EventID,
			ChannelID:     ch.ChannelID,
			ArtifactID:    event.ArtifactID,
			PublisherNode: event.Publisher.NodeID,
			MemoryID:      mem.ID,
			SkippedReason: reason,
		}); err != nil {
			return result, err
		}
		result.Imports = append(result.Imports, channelSyncImport{
			EventID:    event.EventID,
			ArtifactID: event.ArtifactID,
			MemoryID:   mem.ID,
			Reason:     reason,
		})
	}
	return result, nil
}

func readChannelRoster(ctx context.Context, ch store.ChannelSubscription, identity store.ChannelIdentity) ([]channelpkg.RosterCard, error) {
	if isRelayTarget(ch.Path) {
		return relayReadRoster(ctx, ch, identity)
	}
	return channelpkg.ReadRoster(ch.Path)
}

func readChannelEvents(ctx context.Context, ch store.ChannelSubscription, identity store.ChannelIdentity) ([]channelpkg.MemoryArtifactEvent, error) {
	if isRelayTarget(ch.Path) {
		return relayReadEvents(ctx, ch, identity)
	}
	return channelpkg.ReadEvents(ch.Path)
}

func appendChannelEvent(ctx context.Context, ch store.ChannelSubscription, identity store.ChannelIdentity, event channelpkg.MemoryArtifactEvent) (bool, error) {
	if isRelayTarget(ch.Path) {
		return relayAppendEvent(ctx, ch, identity, event)
	}
	return channelpkg.AppendEventIfMissing(ch.Path, event)
}

func remoteArtifactMetadata(event channelpkg.MemoryArtifactEvent) (string, error) {
	metadata := map[string]any{}
	if strings.TrimSpace(event.MetadataJSON) != "" && json.Valid([]byte(event.MetadataJSON)) {
		var original map[string]any
		if err := json.Unmarshal([]byte(event.MetadataJSON), &original); err == nil {
			metadata["original_metadata"] = original
		} else {
			metadata["original_metadata_json"] = event.MetadataJSON
		}
	}
	metadata["remote_artifact"] = map[string]any{
		"event_id":           event.EventID,
		"artifact_id":        event.ArtifactID,
		"channel_id":         event.ChannelID,
		"publisher_node":     event.Publisher.NodeID,
		"publisher_agent":    event.Publisher.Agent,
		"source_memory_id":   event.SourceMemoryID,
		"source_memory_hash": event.SourceMemoryHash,
		"content_hash":       event.ContentHash,
		"payload_hash":       event.PayloadHash,
		"subjects":           event.Subjects,
		"published_at":       event.PublishedAt,
		"source_scope":       event.Scope,
	}
	data, err := json.Marshal(metadata)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func publishRoleSet(roles []string, all bool) map[string]bool {
	out := map[string]bool{}
	if all {
		return out
	}
	if len(roles) == 0 {
		roles = []string{"decision", "adr", "constraint", "preference", "rule", "handoff"}
	}
	for _, role := range roles {
		for _, part := range strings.Split(role, ",") {
			part = strings.ToLower(strings.TrimSpace(part))
			if part != "" {
				out[part] = true
			}
		}
	}
	return out
}

func writeChannelJoin(cmd *cobra.Command, result channelJoinResult) error {
	return frontmatter(cmd.OutOrStdout(), []kv{
		{k: "channel_id", v: result.Channel.ChannelID},
		{k: "name", v: result.Channel.Name},
		{k: "path", v: result.Channel.Path},
		{k: "node_id", v: result.Channel.NodeID},
		{k: "agent", v: result.Channel.Agent},
		{k: "scope", v: result.Channel.ScopeKind},
		{k: "scope_id", v: result.Channel.ScopeID},
		{k: "roster_count", v: fmt.Sprintf("%d", len(result.Roster))},
		{k: "artifact_count", v: fmt.Sprintf("%d", len(result.ArtifactIndex))},
	}, channelRosterBody(result.Roster, result.ArtifactIndex))
}

func writeChannelRoster(cmd *cobra.Command, results []channelJoinResult) error {
	var b strings.Builder
	totalRoster := 0
	totalArtifacts := 0
	for _, result := range results {
		totalRoster += len(result.Roster)
		totalArtifacts += len(result.ArtifactIndex)
		fmt.Fprintf(&b, "## Channel %s\n", firstNonEmpty(result.Channel.Name, result.Channel.ChannelID))
		fmt.Fprintf(&b, "channel_id: %s\npath: %s\n\n", result.Channel.ChannelID, result.Channel.Path)
		b.WriteString(channelRosterBody(result.Roster, result.ArtifactIndex))
		if !strings.HasSuffix(b.String(), "\n") {
			b.WriteByte('\n')
		}
	}
	return frontmatter(cmd.OutOrStdout(), []kv{
		{k: "channel_count", v: fmt.Sprintf("%d", len(results))},
		{k: "roster_count", v: fmt.Sprintf("%d", totalRoster)},
		{k: "artifact_count", v: fmt.Sprintf("%d", totalArtifacts)},
	}, b.String())
}

func channelRosterBody(roster []channelpkg.RosterCard, artifacts []channelpkg.ArtifactSummary) string {
	var b strings.Builder
	b.WriteString("## Roster\n")
	if len(roster) == 0 {
		b.WriteString("No verified peers found.\n")
	} else {
		for _, card := range roster {
			fmt.Fprintf(&b, "- %s agent=%s last_seen=%s scopes=%s\n",
				card.NodeID, firstNonEmpty(card.Agent, "unknown"), card.LastSeen, rosterScopes(card.Scopes))
		}
	}
	b.WriteString("\n## Artifact Index\n")
	if len(artifacts) == 0 {
		b.WriteString("No artifacts published yet.\n")
	} else {
		for _, artifact := range artifacts {
			fmt.Fprintf(&b, "- %s role=%s claim_key=%s publisher=%s summary=%q\n",
				artifact.ArtifactID, artifact.Role, artifact.ClaimKey, artifact.Publisher, artifact.Summary)
		}
	}
	return b.String()
}

func channelPublishedBlocks(events []channelpkg.MemoryArtifactEvent) string {
	var b strings.Builder
	for _, event := range events {
		fmt.Fprintf(&b, "## %s\n", event.ArtifactID)
		fmt.Fprintf(&b, "event_id: %s\nrole: %s\nclaim_key: %s\npublisher: %s\n\n",
			event.EventID, event.Role, event.ClaimKey, event.Publisher.NodeID)
		b.WriteString(truncateText(event.Content, 500))
		if !strings.HasSuffix(b.String(), "\n") {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func writeChannelSync(cmd *cobra.Command, results []channelSyncResult) error {
	var b strings.Builder
	imported := 0
	for _, result := range results {
		imported += result.Imported
		fmt.Fprintf(&b, "## Channel %s\n", result.ChannelID)
		fmt.Fprintf(&b, "imported: %d\nskipped_self: %d\nskipped_duplicate: %d\nskipped_invalid: %d\n\n",
			result.Imported, result.SkippedSelf, result.SkippedDuplicate, result.SkippedInvalid)
		for _, item := range result.Imports {
			fmt.Fprintf(&b, "- %s -> %s", item.ArtifactID, item.MemoryID)
			if item.Reason != "" {
				fmt.Fprintf(&b, " (%s)", item.Reason)
			}
			b.WriteByte('\n')
		}
	}
	return frontmatter(cmd.OutOrStdout(), []kv{
		{k: "channel_count", v: fmt.Sprintf("%d", len(results))},
		{k: "imported", v: fmt.Sprintf("%d", imported)},
	}, b.String())
}

func rosterScopes(scopes []channelpkg.ScopeRef) string {
	parts := make([]string, 0, len(scopes))
	for _, sc := range scopes {
		parts = append(parts, sc.Kind+":"+sc.ID)
	}
	return strings.Join(parts, ",")
}
