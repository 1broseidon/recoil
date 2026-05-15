package cmd

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	channelpkg "github.com/1broseidon/recoil/internal/channel"
	"github.com/spf13/cobra"
)

const defaultRelayAddr = ":8787"

type relayServeOptions struct {
	addr string
	data string
}

type relayInviteOptions struct {
	data     string
	channel  string
	relayURL string
	ttl      time.Duration
}

type relayInvite struct {
	Token     string `json:"token"`
	ChannelID string `json:"channel_id"`
	CreatedAt string `json:"created_at"`
	ExpiresAt string `json:"expires_at"`
	UsedAt    string `json:"used_at,omitempty"`
}

type relayInviteResponse struct {
	Invite   relayInvite                `json:"invite"`
	Manifest channelpkg.ChannelManifest `json:"manifest"`
	JoinURL  string                     `json:"join_url,omitempty"`
}

type relayEventsResponse struct {
	Events []channelpkg.MemoryArtifactEvent `json:"events"`
	Cursor int                              `json:"cursor"`
}

type relayAppendResponse struct {
	Duplicate bool `json:"duplicate"`
}

func newRelayCommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "relay",
		Short: "Run or administer a dumb artifact relay",
		Long: `Run or administer a dumb channel relay.

The relay stores signed roster cards and signed memory artifact events. It does
not search memory, merge state, or own truth; Recoil clients keep local memory
and use the relay only as a durable machine-to-machine pipe.`,
	}
	c.AddCommand(newRelayServeCommand())
	c.AddCommand(newRelayInviteCommand())
	return c
}

func newRelayServeCommand() *cobra.Command {
	var serveOpts relayServeOptions
	c := &cobra.Command{
		Use:   "serve",
		Short: "Serve a self-hosted channel relay",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			srv := &http.Server{
				Addr:              serveOpts.addr,
				Handler:           newRelayHandler(serveOpts.data),
				ReadHeaderTimeout: 5 * time.Second,
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "recoil relay listening on %s with data %s\n", serveOpts.addr, serveOpts.data)
			err := srv.ListenAndServe()
			if err == http.ErrServerClosed {
				return nil
			}
			return err
		},
	}
	c.Flags().StringVar(&serveOpts.addr, "addr", defaultRelayAddr, "address to listen on")
	c.Flags().StringVar(&serveOpts.data, "data", "/data", "relay data directory")
	return c
}

func newRelayInviteCommand() *cobra.Command {
	var inviteOpts relayInviteOptions
	c := &cobra.Command{
		Use:   "invite",
		Short: "Create a one-time channel registration invite",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			invite, manifest, err := createRelayInvite(inviteOpts.data, inviteOpts.channel, inviteOpts.ttl)
			if err != nil {
				return err
			}
			joinURL := joinURLForInvite(inviteOpts.relayURL, invite.Token)
			result := relayInviteResponse{
				Invite:   invite,
				Manifest: manifest,
				JoinURL:  joinURL,
			}
			if opts.json {
				return writeJSON(cmd.OutOrStdout(), "relay_invite_result", result)
			}
			return frontmatter(cmd.OutOrStdout(), []kv{
				{k: "channel_id", v: invite.ChannelID},
				{k: "channel_name", v: manifest.Name},
				{k: "token", v: invite.Token},
				{k: "expires_at", v: invite.ExpiresAt},
				{k: "join_url", v: joinURL},
			}, fmt.Sprintf("recoil channel join %s\n", joinURL))
		},
	}
	c.Flags().StringVar(&inviteOpts.data, "data", "/data", "relay data directory")
	c.Flags().StringVar(&inviteOpts.channel, "channel", "default", "channel name or id")
	c.Flags().StringVar(&inviteOpts.relayURL, "relay-url", "http://localhost:8787", "externally reachable relay URL")
	c.Flags().DurationVar(&inviteOpts.ttl, "ttl", 24*time.Hour, "invite lifetime")
	return c
}

func newRelayHandler(dataDir string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeRelayJSON(w, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/v1/invites/", func(w http.ResponseWriter, r *http.Request) {
		handleRelayInvite(w, r, dataDir)
	})
	mux.HandleFunc("/v1/channels/", func(w http.ResponseWriter, r *http.Request) {
		handleRelayChannel(w, r, dataDir)
	})
	return mux
}

func handleRelayInvite(w http.ResponseWriter, r *http.Request, dataDir string) {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/invites/"), "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "invite token is required", http.StatusBadRequest)
		return
	}
	token := parts[0]
	invite, err := loadRelayInvite(dataDir, token)
	if err != nil {
		http.Error(w, "invite not found", http.StatusNotFound)
		return
	}
	if err := validateRelayInvite(invite); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	channelDir, manifest, err := relayChannelByID(dataDir, invite.ChannelID)
	if err != nil {
		http.Error(w, "channel not found", http.StatusNotFound)
		return
	}
	if len(parts) == 1 && r.Method == http.MethodGet {
		writeRelayJSON(w, relayInviteResponse{Invite: invite, Manifest: manifest})
		return
	}
	if len(parts) == 2 && parts[1] == "join" && r.Method == http.MethodPost {
		body, err := readRelayBody(r, 1<<20)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var card channelpkg.RosterCard
		if err := json.Unmarshal(body, &card); err != nil {
			http.Error(w, "invalid roster card", http.StatusBadRequest)
			return
		}
		if card.ChannelID != invite.ChannelID {
			http.Error(w, "roster card channel mismatch", http.StatusBadRequest)
			return
		}
		if err := channelpkg.VerifyRosterCard(card); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		if err := consumeRelayInvite(dataDir, token); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		if err := channelpkg.WriteSignedRosterCard(channelDir, card); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		invite.UsedAt = time.Now().UTC().Format(time.RFC3339)
		if err := writeConsumedInvite(dataDir, invite); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeRelayJSON(w, map[string]string{"status": "joined", "channel_id": manifest.ChannelID})
		return
	}
	http.Error(w, "not found", http.StatusNotFound)
}

func handleRelayChannel(w http.ResponseWriter, r *http.Request, dataDir string) {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/channels/"), "/"), "/")
	if len(parts) < 2 || parts[0] == "" {
		http.Error(w, "channel id and resource are required", http.StatusBadRequest)
		return
	}
	channelID, resource := parts[0], parts[1]
	channelDir, manifest, err := relayChannelByID(dataDir, channelID)
	if err != nil {
		http.Error(w, "channel not found", http.StatusNotFound)
		return
	}
	if resource == "manifest" && r.Method == http.MethodGet {
		writeRelayJSON(w, manifest)
		return
	}

	body, err := readRelayBody(r, 8<<20)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	card, err := authenticateRelayRequest(r, body, channelDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	_ = card

	switch resource {
	case "roster":
		if r.Method == http.MethodGet {
			roster, err := channelpkg.ReadRoster(channelDir)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeRelayJSON(w, roster)
			return
		}
		if r.Method == http.MethodPost {
			var update channelpkg.RosterCard
			if err := json.Unmarshal(body, &update); err != nil {
				http.Error(w, "invalid roster card", http.StatusBadRequest)
				return
			}
			if update.ChannelID != manifest.ChannelID || update.NodeID != card.NodeID {
				http.Error(w, "roster update mismatch", http.StatusForbidden)
				return
			}
			if err := channelpkg.WriteSignedRosterCard(channelDir, update); err != nil {
				http.Error(w, err.Error(), http.StatusForbidden)
				return
			}
			writeRelayJSON(w, map[string]string{"status": "updated"})
			return
		}
	case "events":
		if r.Method == http.MethodGet {
			events, err := channelpkg.ReadEvents(channelDir)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			after := parseRelayCursor(r.URL.Query().Get("after"))
			if after > len(events) {
				after = len(events)
			}
			writeRelayJSON(w, relayEventsResponse{Events: events[after:], Cursor: len(events)})
			return
		}
		if r.Method == http.MethodPost {
			var event channelpkg.MemoryArtifactEvent
			if err := json.Unmarshal(body, &event); err != nil {
				http.Error(w, "invalid artifact event", http.StatusBadRequest)
				return
			}
			if event.ChannelID != manifest.ChannelID || event.Publisher.NodeID != card.NodeID {
				http.Error(w, "artifact publisher mismatch", http.StatusForbidden)
				return
			}
			if err := channelpkg.VerifyMemoryArtifactEvent(event); err != nil {
				http.Error(w, err.Error(), http.StatusForbidden)
				return
			}
			duplicate, err := channelpkg.AppendEventIfMissing(channelDir, event)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeRelayJSON(w, relayAppendResponse{Duplicate: duplicate})
			return
		}
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

func createRelayInvite(dataDir, channelName string, ttl time.Duration) (relayInvite, channelpkg.ChannelManifest, error) {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	channelDir, manifest, err := ensureRelayChannel(dataDir, channelName)
	if err != nil {
		return relayInvite{}, manifest, err
	}
	_ = channelDir
	token := randomRelayToken()
	now := time.Now().UTC()
	invite := relayInvite{
		Token:     token,
		ChannelID: manifest.ChannelID,
		CreatedAt: now.Format(time.RFC3339),
		ExpiresAt: now.Add(ttl).Format(time.RFC3339),
	}
	return invite, manifest, saveRelayInvite(dataDir, invite)
}

func ensureRelayChannel(dataDir, name string) (string, channelpkg.ChannelManifest, error) {
	if strings.TrimSpace(name) == "" {
		name = "default"
	}
	if dir, manifest, err := relayChannelByNameOrID(dataDir, name); err == nil {
		return dir, manifest, nil
	}
	dir := filepath.Join(relayChannelsDir(dataDir), safeRelayName(name))
	manifest, path, err := channelpkg.EnsureChannel(dir, name)
	return path, manifest, err
}

func relayChannelByID(dataDir, channelID string) (string, channelpkg.ChannelManifest, error) {
	return relayChannelByNameOrID(dataDir, channelID)
}

func relayChannelByNameOrID(dataDir, selector string) (string, channelpkg.ChannelManifest, error) {
	entries, err := os.ReadDir(relayChannelsDir(dataDir))
	if err != nil {
		return "", channelpkg.ChannelManifest{}, err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(relayChannelsDir(dataDir), entry.Name())
		manifest, err := channelpkg.LoadManifest(dir)
		if err != nil {
			continue
		}
		if manifest.ChannelID == selector || manifest.Name == selector {
			return dir, manifest, nil
		}
	}
	return "", channelpkg.ChannelManifest{}, os.ErrNotExist
}

func relayChannelsDir(dataDir string) string {
	return filepath.Join(dataDir, "channels")
}

func relayInvitesDir(dataDir string) string {
	return filepath.Join(dataDir, "invites")
}

func saveRelayInvite(dataDir string, invite relayInvite) error {
	if err := os.MkdirAll(relayInvitesDir(dataDir), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(invite, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(filepath.Join(relayInvitesDir(dataDir), invite.Token+".json"), data, 0o600)
}

func consumeRelayInvite(dataDir, token string) error {
	src := filepath.Join(relayInvitesDir(dataDir), token+".json")
	dst := filepath.Join(relayInvitesDir(dataDir), token+".used.json")
	if err := os.Rename(src, dst); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("invite has already been used")
		}
		return err
	}
	return nil
}

func writeConsumedInvite(dataDir string, invite relayInvite) error {
	data, err := json.MarshalIndent(invite, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(filepath.Join(relayInvitesDir(dataDir), invite.Token+".used.json"), data, 0o600)
}

func loadRelayInvite(dataDir, token string) (relayInvite, error) {
	var invite relayInvite
	data, err := os.ReadFile(filepath.Join(relayInvitesDir(dataDir), token+".json"))
	if err != nil {
		return invite, err
	}
	return invite, json.Unmarshal(data, &invite)
}

func validateRelayInvite(invite relayInvite) error {
	if strings.TrimSpace(invite.UsedAt) != "" {
		return fmt.Errorf("invite has already been used")
	}
	expires, err := time.Parse(time.RFC3339, invite.ExpiresAt)
	if err != nil {
		return fmt.Errorf("invite expiry is invalid")
	}
	if time.Now().UTC().After(expires) {
		return fmt.Errorf("invite has expired")
	}
	return nil
}

func authenticateRelayRequest(r *http.Request, body []byte, channelDir string) (channelpkg.RosterCard, error) {
	nodeID := strings.TrimSpace(r.Header.Get("X-Recoil-Node"))
	timestamp := strings.TrimSpace(r.Header.Get("X-Recoil-Timestamp"))
	signature := strings.TrimSpace(r.Header.Get("X-Recoil-Signature"))
	if nodeID == "" || timestamp == "" || signature == "" {
		return channelpkg.RosterCard{}, fmt.Errorf("missing recoil request signature")
	}
	ts, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		return channelpkg.RosterCard{}, fmt.Errorf("invalid recoil timestamp")
	}
	if delta := time.Since(ts); delta > 15*time.Minute || delta < -15*time.Minute {
		return channelpkg.RosterCard{}, fmt.Errorf("stale recoil timestamp")
	}
	card, err := relayRosterCard(channelDir, nodeID)
	if err != nil {
		return channelpkg.RosterCard{}, fmt.Errorf("node is not registered for channel")
	}
	if err := channelpkg.VerifyRequest(r.Method, relayRequestTarget(r), timestamp, body, card.PublicKey, signature); err != nil {
		return channelpkg.RosterCard{}, err
	}
	return card, nil
}

func relayRosterCard(channelDir, nodeID string) (channelpkg.RosterCard, error) {
	roster, err := channelpkg.ReadRoster(channelDir)
	if err != nil {
		return channelpkg.RosterCard{}, err
	}
	for _, card := range roster {
		if card.NodeID == nodeID {
			return card, nil
		}
	}
	return channelpkg.RosterCard{}, os.ErrNotExist
}

func relayRequestTarget(r *http.Request) string {
	target := r.URL.EscapedPath()
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	return target
}

func readRelayBody(r *http.Request, maxBytes int64) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	defer r.Body.Close()
	return io.ReadAll(io.LimitReader(r.Body, maxBytes))
}

func parseRelayCursor(raw string) int {
	if raw == "" {
		return 0
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 0
	}
	return value
}

func writeRelayJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(value)
}

func joinURLForInvite(relayURL, token string) string {
	relayURL = strings.TrimRight(strings.TrimSpace(relayURL), "/")
	if relayURL == "" {
		return ""
	}
	return relayURL + "/v1/invites/" + token
}

func randomRelayToken() string {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func safeRelayName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "default"
	}
	return out
}

func shutdownRelayServer(ctx context.Context, srv *http.Server) error {
	if srv == nil {
		return nil
	}
	return srv.Shutdown(ctx)
}
