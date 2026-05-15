package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	channelpkg "github.com/1broseidon/recoil/internal/channel"
	"github.com/spf13/cobra"
)

const defaultRelayStaleAfter = 30 * 24 * time.Hour

type relayStatusOptions struct {
	data       string
	staleAfter time.Duration
}

type relaySetupOptions struct {
	addr     string
	data     string
	channel  string
	relayURL string
	ttl      time.Duration
}

type relayDoctorOptions struct {
	addr       string
	data       string
	relayURL   string
	staleAfter time.Duration
}

type relayInviteAdminOptions struct {
	data  string
	token string
}

type relayMemberOptions struct {
	data       string
	channel    string
	nodeID     string
	staleAfter time.Duration
}

type relayChannelOptions struct {
	data    string
	channel string
}

type relayStatusResult struct {
	ChannelCount  int                   `json:"channel_count"`
	PeerCount     int                   `json:"peer_count"`
	EventCount    int                   `json:"event_count"`
	LastPublish   string                `json:"last_publish,omitempty"`
	DiskFootprint int64                 `json:"disk_footprint"`
	Channels      []relayChannelSummary `json:"channels"`
	Invites       relayInviteSummary    `json:"invites"`
}

type relayChannelSummary struct {
	ChannelID   string               `json:"channel_id"`
	Name        string               `json:"name,omitempty"`
	PeerCount   int                  `json:"peer_count"`
	EventCount  int                  `json:"event_count"`
	LastPublish string               `json:"last_publish,omitempty"`
	LastSeen    []relayMemberSummary `json:"last_seen,omitempty"`
	StalePeers  []relayMemberSummary `json:"stale_peers,omitempty"`
}

type relayMemberSummary struct {
	NodeID   string `json:"node_id"`
	Agent    string `json:"agent,omitempty"`
	LastSeen string `json:"last_seen,omitempty"`
	Stale    bool   `json:"stale,omitempty"`
}

type relayInviteSummary struct {
	Outstanding int `json:"outstanding"`
	Consumed    int `json:"consumed"`
	Revoked     int `json:"revoked"`
	Expired     int `json:"expired"`
}

type relayInviteRecord struct {
	Invite relayInvite `json:"invite"`
	Status string      `json:"status"`
	Path   string      `json:"path,omitempty"`
}

type relaySetupResult struct {
	Invite        relayInvite                `json:"invite"`
	Manifest      channelpkg.ChannelManifest `json:"manifest"`
	JoinURL       string                     `json:"join_url"`
	ServeCommand  string                     `json:"serve_command"`
	DockerCommand string                     `json:"docker_command"`
	Warnings      []string                   `json:"warnings,omitempty"`
}

type relayDoctorResult struct {
	Status   relayStatusResult `json:"status"`
	Health   string            `json:"health"`
	Warnings []string          `json:"warnings,omitempty"`
}

func newRelayStatusCommand() *cobra.Command {
	var statusOpts relayStatusOptions
	c := &cobra.Command{
		Use:   "status",
		Short: "Show relay channels, peers, events, and disk footprint",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := inspectRelayStatus(statusOpts.data, statusOpts.staleAfter)
			if err != nil {
				return err
			}
			if opts.json {
				return writeJSON(cmd.OutOrStdout(), "relay_status_result", result)
			}
			return writeRelayStatus(cmd, result)
		},
	}
	c.Flags().StringVar(&statusOpts.data, "data", "/data", "relay data directory")
	c.Flags().DurationVar(&statusOpts.staleAfter, "stale-after", defaultRelayStaleAfter, "mark members stale after this duration")
	return c
}

func newRelayInviteListCommand() *cobra.Command {
	var inviteOpts relayInviteAdminOptions
	c := &cobra.Command{
		Use:   "list",
		Short: "List outstanding, consumed, and revoked relay invites",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			records, err := listRelayInvites(inviteOpts.data)
			if err != nil {
				return err
			}
			if opts.json {
				return writeJSON(cmd.OutOrStdout(), "relay_invite_list_result", records)
			}
			var b strings.Builder
			for _, record := range records {
				fmt.Fprintf(&b, "- %s status=%s channel=%s expires_at=%s\n", record.Invite.Token, record.Status, record.Invite.ChannelID, record.Invite.ExpiresAt)
			}
			return frontmatter(cmd.OutOrStdout(), []kv{{k: "invite_count", v: fmt.Sprintf("%d", len(records))}}, b.String())
		},
	}
	c.Flags().StringVar(&inviteOpts.data, "data", "/data", "relay data directory")
	return c
}

func newRelayInviteStatusCommand() *cobra.Command {
	var inviteOpts relayInviteAdminOptions
	c := &cobra.Command{
		Use:   "status <token>",
		Short: "Show one relay invite status",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			record, err := relayInviteStatus(inviteOpts.data, args[0])
			if err != nil {
				return err
			}
			if opts.json {
				return writeJSON(cmd.OutOrStdout(), "relay_invite_status_result", record)
			}
			return frontmatter(cmd.OutOrStdout(), []kv{
				{k: "token", v: record.Invite.Token},
				{k: "status", v: record.Status},
				{k: "channel_id", v: record.Invite.ChannelID},
				{k: "expires_at", v: record.Invite.ExpiresAt},
				{k: "used_at", v: record.Invite.UsedAt},
			}, "")
		},
	}
	c.Flags().StringVar(&inviteOpts.data, "data", "/data", "relay data directory")
	return c
}

func newRelayInviteRevokeCommand() *cobra.Command {
	var inviteOpts relayInviteAdminOptions
	c := &cobra.Command{
		Use:   "revoke <token>",
		Short: "Revoke an outstanding relay invite",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			record, err := revokeRelayInvite(inviteOpts.data, args[0])
			if err != nil {
				return err
			}
			if opts.json {
				return writeJSON(cmd.OutOrStdout(), "relay_invite_revoke_result", record)
			}
			return frontmatter(cmd.OutOrStdout(), []kv{
				{k: "token", v: record.Invite.Token},
				{k: "status", v: record.Status},
			}, "")
		},
	}
	c.Flags().StringVar(&inviteOpts.data, "data", "/data", "relay data directory")
	return c
}

func newRelayMemberCommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "member",
		Short: "Manage relay roster membership",
	}
	c.AddCommand(newRelayMemberListCommand(), newRelayMemberKickCommand())
	return c
}

func newRelayMemberListCommand() *cobra.Command {
	var memberOpts relayMemberOptions
	c := &cobra.Command{
		Use:   "list",
		Short: "List relay members",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			channels, err := relayMemberChannels(memberOpts.data, memberOpts.channel, memberOpts.staleAfter)
			if err != nil {
				return err
			}
			if opts.json {
				return writeJSON(cmd.OutOrStdout(), "relay_member_list_result", channels)
			}
			var b strings.Builder
			count := 0
			for _, ch := range channels {
				fmt.Fprintf(&b, "## Channel %s\n", firstNonEmpty(ch.Name, ch.ChannelID))
				for _, member := range ch.LastSeen {
					count++
					fmt.Fprintf(&b, "- %s agent=%s last_seen=%s stale=%t\n", member.NodeID, firstNonEmpty(member.Agent, "unknown"), member.LastSeen, member.Stale)
				}
			}
			return frontmatter(cmd.OutOrStdout(), []kv{
				{k: "channel_count", v: fmt.Sprintf("%d", len(channels))},
				{k: "member_count", v: fmt.Sprintf("%d", count)},
			}, b.String())
		},
	}
	c.Flags().StringVar(&memberOpts.data, "data", "/data", "relay data directory")
	c.Flags().StringVar(&memberOpts.channel, "channel", "", "channel id or name")
	c.Flags().DurationVar(&memberOpts.staleAfter, "stale-after", defaultRelayStaleAfter, "mark members stale after this duration")
	return c
}

func newRelayMemberKickCommand() *cobra.Command {
	var memberOpts relayMemberOptions
	c := &cobra.Command{
		Use:   "kick <node-id>",
		Short: "Remove a member roster card from a relay channel",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			memberOpts.nodeID = args[0]
			channelID, err := kickRelayMember(memberOpts.data, memberOpts.channel, memberOpts.nodeID)
			if err != nil {
				return err
			}
			if opts.json {
				return writeJSON(cmd.OutOrStdout(), "relay_member_kick_result", map[string]string{"channel_id": channelID, "node_id": memberOpts.nodeID, "status": "kicked"})
			}
			return frontmatter(cmd.OutOrStdout(), []kv{
				{k: "channel_id", v: channelID},
				{k: "node_id", v: memberOpts.nodeID},
				{k: "status", v: "kicked"},
			}, "")
		},
	}
	c.Flags().StringVar(&memberOpts.data, "data", "/data", "relay data directory")
	c.Flags().StringVar(&memberOpts.channel, "channel", "", "channel id or name")
	return c
}

func newRelayChannelCommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "channel",
		Short: "Create or inspect relay channels",
	}
	c.AddCommand(newRelayChannelCreateCommand(), newRelayChannelInspectCommand())
	return c
}

func newRelayChannelCreateCommand() *cobra.Command {
	var channelOpts relayChannelOptions
	c := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a relay channel",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, manifest, err := ensureRelayChannel(channelOpts.data, args[0])
			if err != nil {
				return err
			}
			if opts.json {
				return writeJSON(cmd.OutOrStdout(), "relay_channel_create_result", manifest)
			}
			return frontmatter(cmd.OutOrStdout(), []kv{
				{k: "channel_id", v: manifest.ChannelID},
				{k: "name", v: manifest.Name},
			}, "")
		},
	}
	c.Flags().StringVar(&channelOpts.data, "data", "/data", "relay data directory")
	return c
}

func newRelayChannelInspectCommand() *cobra.Command {
	var channelOpts relayChannelOptions
	c := &cobra.Command{
		Use:   "inspect <channel>",
		Short: "Inspect a relay channel",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := inspectRelayChannel(channelOpts.data, args[0], defaultRelayStaleAfter)
			if err != nil {
				return err
			}
			if opts.json {
				return writeJSON(cmd.OutOrStdout(), "relay_channel_inspect_result", result)
			}
			return writeRelayChannelInspect(cmd, result)
		},
	}
	c.Flags().StringVar(&channelOpts.data, "data", "/data", "relay data directory")
	return c
}

func newRelaySetupCommand() *cobra.Command {
	var setupOpts relaySetupOptions
	c := &cobra.Command{
		Use:   "setup",
		Short: "Bootstrap a self-hosted relay data directory and first invite",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(setupOpts.relayURL) == "" {
				setupOpts.relayURL = relayURLForAddr(setupOpts.addr)
			}
			invite, manifest, err := createRelayInvite(setupOpts.data, setupOpts.channel, setupOpts.ttl)
			if err != nil {
				return err
			}
			joinURL := joinURLForInvite(setupOpts.relayURL, invite.Token)
			result := relaySetupResult{
				Invite:        invite,
				Manifest:      manifest,
				JoinURL:       joinURL,
				ServeCommand:  fmt.Sprintf("recoil relay serve --addr %s --data %s", setupOpts.addr, setupOpts.data),
				DockerCommand: "docker run -p 8787:8787 -v recoil-relay:/data recoil-relay",
				Warnings:      relayURLWarnings(setupOpts.relayURL),
			}
			if opts.json {
				return writeJSON(cmd.OutOrStdout(), "relay_setup_result", result)
			}
			var b strings.Builder
			fmt.Fprintf(&b, "%s\n\n", result.ServeCommand)
			fmt.Fprintf(&b, "%s\n\n", result.DockerCommand)
			fmt.Fprintf(&b, "recoil channel join %s\n", result.JoinURL)
			for _, warning := range result.Warnings {
				fmt.Fprintf(&b, "\nwarning: %s\n", warning)
			}
			return frontmatter(cmd.OutOrStdout(), []kv{
				{k: "channel_id", v: manifest.ChannelID},
				{k: "channel_name", v: manifest.Name},
				{k: "join_url", v: result.JoinURL},
				{k: "data", v: setupOpts.data},
			}, b.String())
		},
	}
	c.Flags().StringVar(&setupOpts.addr, "addr", defaultRelayAddr, "address the relay should listen on")
	c.Flags().StringVar(&setupOpts.data, "data", "./recoil-relay-data", "relay data directory")
	c.Flags().StringVar(&setupOpts.channel, "channel", "default", "channel name or id")
	c.Flags().StringVar(&setupOpts.relayURL, "relay-url", "", "externally reachable relay URL")
	c.Flags().DurationVar(&setupOpts.ttl, "ttl", 24*time.Hour, "first invite lifetime")
	return c
}

func newRelayDoctorCommand() *cobra.Command {
	var doctorOpts relayDoctorOptions
	c := &cobra.Command{
		Use:   "doctor",
		Short: "Explain relay health and likely operator issues",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			status, err := inspectRelayStatus(doctorOpts.data, doctorOpts.staleAfter)
			if err != nil {
				return err
			}
			result := relayDoctorResult{
				Status:   status,
				Health:   relayHealth(doctorOpts.relayURL),
				Warnings: relayURLWarnings(doctorOpts.relayURL),
			}
			if opts.json {
				return writeJSON(cmd.OutOrStdout(), "relay_doctor_result", result)
			}
			var b strings.Builder
			fmt.Fprintf(&b, "running: %s\n", result.Health)
			fmt.Fprintf(&b, "channels: %d\npeers: %d\nevents: %d\nlast_activity: %s\n", status.ChannelCount, status.PeerCount, status.EventCount, firstNonEmpty(status.LastPublish, "none"))
			for _, warning := range result.Warnings {
				fmt.Fprintf(&b, "warning: %s\n", warning)
			}
			if doctorOpts.relayURL == "" {
				fmt.Fprintf(&b, "hint: pass --relay-url %s to check /healthz\n", relayURLForAddr(doctorOpts.addr))
			}
			return frontmatter(cmd.OutOrStdout(), []kv{
				{k: "data", v: doctorOpts.data},
				{k: "health", v: result.Health},
				{k: "channel_count", v: fmt.Sprintf("%d", status.ChannelCount)},
				{k: "peer_count", v: fmt.Sprintf("%d", status.PeerCount)},
				{k: "event_count", v: fmt.Sprintf("%d", status.EventCount)},
			}, b.String())
		},
	}
	c.Flags().StringVar(&doctorOpts.addr, "addr", defaultRelayAddr, "expected relay listen address")
	c.Flags().StringVar(&doctorOpts.data, "data", "/data", "relay data directory")
	c.Flags().StringVar(&doctorOpts.relayURL, "relay-url", "", "relay URL to check")
	c.Flags().DurationVar(&doctorOpts.staleAfter, "stale-after", defaultRelayStaleAfter, "mark members stale after this duration")
	return c
}

func inspectRelayStatus(dataDir string, staleAfter time.Duration) (relayStatusResult, error) {
	var result relayStatusResult
	channels, err := relayChannelSummaries(dataDir, "", staleAfter)
	if err != nil {
		return result, err
	}
	result.Channels = channels
	result.ChannelCount = len(channels)
	for _, ch := range channels {
		result.PeerCount += ch.PeerCount
		result.EventCount += ch.EventCount
		if ch.LastPublish > result.LastPublish {
			result.LastPublish = ch.LastPublish
		}
	}
	result.Invites = summarizeRelayInvites(dataDir)
	result.DiskFootprint = relayDiskFootprint(dataDir)
	return result, nil
}

func inspectRelayChannel(dataDir, selector string, staleAfter time.Duration) (relayChannelSummary, error) {
	channels, err := relayChannelSummaries(dataDir, selector, staleAfter)
	if err != nil {
		return relayChannelSummary{}, err
	}
	if len(channels) == 0 {
		return relayChannelSummary{}, fmt.Errorf("relay channel %q not found", selector)
	}
	return channels[0], nil
}

func relayMemberChannels(dataDir, selector string, staleAfter time.Duration) ([]relayChannelSummary, error) {
	return relayChannelSummaries(dataDir, selector, staleAfter)
}

func relayChannelSummaries(dataDir, selector string, staleAfter time.Duration) ([]relayChannelSummary, error) {
	entries, err := os.ReadDir(relayChannelsDir(dataDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var summaries []relayChannelSummary
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(relayChannelsDir(dataDir), entry.Name())
		manifest, err := channelpkg.LoadManifest(dir)
		if err != nil {
			continue
		}
		if selector != "" && manifest.ChannelID != selector && manifest.Name != selector {
			continue
		}
		roster, _ := channelpkg.ReadRoster(dir)
		events, _ := channelpkg.ReadEvents(dir)
		summary := relayChannelSummary{
			ChannelID:  manifest.ChannelID,
			Name:       manifest.Name,
			PeerCount:  len(roster),
			EventCount: len(events),
		}
		for _, event := range events {
			if event.PublishedAt > summary.LastPublish {
				summary.LastPublish = event.PublishedAt
			}
		}
		for _, card := range roster {
			member := relayMemberSummary{
				NodeID:   card.NodeID,
				Agent:    card.Agent,
				LastSeen: card.LastSeen,
				Stale:    relayLastSeenStale(card.LastSeen, staleAfter),
			}
			summary.LastSeen = append(summary.LastSeen, member)
			if member.Stale {
				summary.StalePeers = append(summary.StalePeers, member)
			}
		}
		sort.Slice(summary.LastSeen, func(i, j int) bool {
			return summary.LastSeen[i].LastSeen > summary.LastSeen[j].LastSeen
		})
		summaries = append(summaries, summary)
	}
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].Name < summaries[j].Name
	})
	return summaries, nil
}

func listRelayInvites(dataDir string) ([]relayInviteRecord, error) {
	entries, err := os.ReadDir(relayInvitesDir(dataDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var records []relayInviteRecord
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(relayInvitesDir(dataDir), entry.Name())
		record, err := readRelayInviteRecord(path)
		if err != nil {
			continue
		}
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].Invite.CreatedAt > records[j].Invite.CreatedAt
	})
	return records, nil
}

func relayInviteStatus(dataDir, token string) (relayInviteRecord, error) {
	for _, suffix := range []string{".json", ".used.json", ".revoked.json"} {
		path := filepath.Join(relayInvitesDir(dataDir), token+suffix)
		record, err := readRelayInviteRecord(path)
		if err == nil {
			return record, nil
		}
	}
	return relayInviteRecord{}, fmt.Errorf("invite %q not found", token)
}

func revokeRelayInvite(dataDir, token string) (relayInviteRecord, error) {
	src := filepath.Join(relayInvitesDir(dataDir), token+".json")
	dst := filepath.Join(relayInvitesDir(dataDir), token+".revoked.json")
	record, err := readRelayInviteRecord(src)
	if err != nil {
		return relayInviteRecord{}, err
	}
	if err := os.Rename(src, dst); err != nil {
		return relayInviteRecord{}, err
	}
	record.Status = "revoked"
	record.Path = dst
	return record, nil
}

func readRelayInviteRecord(path string) (relayInviteRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return relayInviteRecord{}, err
	}
	var invite relayInvite
	if err := json.Unmarshal(data, &invite); err != nil {
		return relayInviteRecord{}, err
	}
	status := "outstanding"
	switch {
	case strings.HasSuffix(path, ".used.json"):
		status = "consumed"
	case strings.HasSuffix(path, ".revoked.json"):
		status = "revoked"
	case relayInviteExpired(invite):
		status = "expired"
	}
	return relayInviteRecord{Invite: invite, Status: status, Path: path}, nil
}

func summarizeRelayInvites(dataDir string) relayInviteSummary {
	records, _ := listRelayInvites(dataDir)
	var summary relayInviteSummary
	for _, record := range records {
		switch record.Status {
		case "consumed":
			summary.Consumed++
		case "revoked":
			summary.Revoked++
		case "expired":
			summary.Expired++
		default:
			summary.Outstanding++
		}
	}
	return summary
}

func kickRelayMember(dataDir, selector, nodeID string) (string, error) {
	if strings.TrimSpace(selector) == "" {
		return "", fmt.Errorf("pass --channel to choose which roster to modify")
	}
	channelDir, manifest, err := relayChannelByNameOrID(dataDir, selector)
	if err != nil {
		return "", err
	}
	path := filepath.Join(channelDir, channelpkg.RosterDirectoryName, strings.TrimSpace(nodeID)+".json")
	if err := os.Remove(path); err != nil {
		return "", err
	}
	return manifest.ChannelID, nil
}

func relayDiskFootprint(dataDir string) int64 {
	var total int64
	_ = filepath.WalkDir(dataDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

func relayInviteExpired(invite relayInvite) bool {
	expires, err := time.Parse(time.RFC3339, invite.ExpiresAt)
	return err == nil && time.Now().UTC().After(expires)
}

func relayLastSeenStale(lastSeen string, staleAfter time.Duration) bool {
	if staleAfter <= 0 || strings.TrimSpace(lastSeen) == "" {
		return false
	}
	seen, err := time.Parse(time.RFC3339, lastSeen)
	return err == nil && time.Since(seen) > staleAfter
}

func relayURLForAddr(addr string) string {
	addr = strings.TrimSpace(addr)
	port := "8787"
	if strings.Contains(addr, ":") {
		parts := strings.Split(addr, ":")
		if last := parts[len(parts)-1]; last != "" {
			port = last
		}
	}
	return "http://localhost:" + port
}

func relayURLWarnings(relayURL string) []string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(relayURL)), "http://") {
		return []string{"relay URL is plain HTTP; use a TLS reverse proxy before exposing it outside a trusted network"}
	}
	return nil
}

func relayHealth(relayURL string) string {
	relayURL = strings.TrimRight(strings.TrimSpace(relayURL), "/")
	if relayURL == "" {
		return "not checked"
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(relayURL + "/healthz")
	if err != nil {
		return "unreachable: " + err.Error()
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return "ok"
	}
	return resp.Status
}

func writeRelayStatus(cmd *cobra.Command, result relayStatusResult) error {
	var b strings.Builder
	for _, ch := range result.Channels {
		fmt.Fprintf(&b, "## Channel %s\n", firstNonEmpty(ch.Name, ch.ChannelID))
		fmt.Fprintf(&b, "channel_id: %s\npeers: %d\nevents: %d\nlast_publish: %s\n", ch.ChannelID, ch.PeerCount, ch.EventCount, firstNonEmpty(ch.LastPublish, "none"))
		for _, member := range ch.LastSeen {
			fmt.Fprintf(&b, "- %s agent=%s last_seen=%s stale=%t\n", member.NodeID, firstNonEmpty(member.Agent, "unknown"), member.LastSeen, member.Stale)
		}
		b.WriteByte('\n')
	}
	return frontmatter(cmd.OutOrStdout(), []kv{
		{k: "channel_count", v: fmt.Sprintf("%d", result.ChannelCount)},
		{k: "peer_count", v: fmt.Sprintf("%d", result.PeerCount)},
		{k: "event_count", v: fmt.Sprintf("%d", result.EventCount)},
		{k: "last_publish", v: firstNonEmpty(result.LastPublish, "none")},
		{k: "disk_footprint_bytes", v: fmt.Sprintf("%d", result.DiskFootprint)},
		{k: "outstanding_invites", v: fmt.Sprintf("%d", result.Invites.Outstanding)},
	}, b.String())
}

func writeRelayChannelInspect(cmd *cobra.Command, result relayChannelSummary) error {
	var b strings.Builder
	for _, member := range result.LastSeen {
		fmt.Fprintf(&b, "- %s agent=%s last_seen=%s stale=%t\n", member.NodeID, firstNonEmpty(member.Agent, "unknown"), member.LastSeen, member.Stale)
	}
	return frontmatter(cmd.OutOrStdout(), []kv{
		{k: "channel_id", v: result.ChannelID},
		{k: "name", v: result.Name},
		{k: "peer_count", v: fmt.Sprintf("%d", result.PeerCount)},
		{k: "event_count", v: fmt.Sprintf("%d", result.EventCount)},
		{k: "last_publish", v: firstNonEmpty(result.LastPublish, "none")},
	}, b.String())
}
