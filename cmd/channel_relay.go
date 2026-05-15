package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	channelpkg "github.com/1broseidon/recoil/internal/channel"
	"github.com/1broseidon/recoil/internal/scope"
	"github.com/1broseidon/recoil/internal/store"
)

func isRelayTarget(target string) bool {
	u, err := url.Parse(strings.TrimSpace(target))
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func joinRelayChannel(ctx context.Context, st *store.Store, identity store.ChannelIdentity, sc scope.Scope, agent, inviteURL string) (channelJoinResult, error) {
	invite, err := relayFetchInvite(ctx, inviteURL)
	if err != nil {
		return channelJoinResult{}, err
	}
	baseURL, err := relayBaseURL(inviteURL)
	if err != nil {
		return channelJoinResult{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	ch := store.ChannelSubscription{
		ChannelID: invite.Manifest.ChannelID,
		Name:      invite.Manifest.Name,
		Path:      baseURL,
		NodeID:    identity.NodeID,
		Agent:     firstNonEmpty(agent, "recoil"),
		ScopeKind: sc.Kind,
		ScopeID:   sc.ID,
		ProjectID: sc.ProjectID,
		SessionID: sc.SessionID,
		JoinedAt:  now,
		UpdatedAt: now,
	}
	card := channelpkg.BuildRosterCard(ch, identity)
	if err := channelpkg.SignRosterCard(&card, identity.PrivateKey); err != nil {
		return channelJoinResult{}, err
	}
	body, err := json.Marshal(card)
	if err != nil {
		return channelJoinResult{}, err
	}
	if err := relayDoUnsigned(ctx, http.MethodPost, strings.TrimRight(inviteURL, "/")+"/join", body, nil); err != nil {
		return channelJoinResult{}, err
	}
	ch, err = st.UpsertChannelSubscription(ctx, store.UpsertChannelSubscriptionParams{
		ChannelID: invite.Manifest.ChannelID,
		Name:      invite.Manifest.Name,
		Path:      baseURL,
		NodeID:    identity.NodeID,
		Agent:     firstNonEmpty(agent, "recoil"),
		ScopeKind: sc.Kind,
		ScopeID:   sc.ID,
		ProjectID: sc.ProjectID,
		SessionID: sc.SessionID,
	})
	if err != nil {
		return channelJoinResult{}, err
	}
	roster, err := relayReadRoster(ctx, ch, identity)
	if err != nil {
		return channelJoinResult{}, err
	}
	events, err := relayReadEvents(ctx, ch, identity)
	if err != nil {
		return channelJoinResult{}, err
	}
	return channelJoinResult{
		Channel:       ch,
		Manifest:      invite.Manifest,
		Roster:        roster,
		ArtifactIndex: channelpkg.ArtifactSummaries(events),
	}, nil
}

func relayFetchInvite(ctx context.Context, inviteURL string) (relayInviteResponse, error) {
	var response relayInviteResponse
	err := relayDoUnsigned(ctx, http.MethodGet, inviteURL, nil, &response)
	return response, err
}

func relayLoadManifest(ctx context.Context, ch store.ChannelSubscription) (channelpkg.ChannelManifest, error) {
	var manifest channelpkg.ChannelManifest
	err := relayDoUnsigned(ctx, http.MethodGet, relayEndpoint(ch, "/v1/channels/"+ch.ChannelID+"/manifest"), nil, &manifest)
	return manifest, err
}

func relayReadRoster(ctx context.Context, ch store.ChannelSubscription, identity store.ChannelIdentity) ([]channelpkg.RosterCard, error) {
	var roster []channelpkg.RosterCard
	err := relayDoSigned(ctx, http.MethodGet, ch, "/v1/channels/"+ch.ChannelID+"/roster", "", identity, nil, &roster)
	return roster, err
}

func relayUpdateRoster(ctx context.Context, ch store.ChannelSubscription, identity store.ChannelIdentity, card channelpkg.RosterCard) error {
	body, err := json.Marshal(card)
	if err != nil {
		return err
	}
	return relayDoSigned(ctx, http.MethodPost, ch, "/v1/channels/"+ch.ChannelID+"/roster", "", identity, body, nil)
}

func relayReadEvents(ctx context.Context, ch store.ChannelSubscription, identity store.ChannelIdentity) ([]channelpkg.MemoryArtifactEvent, error) {
	events, _, err := relayReadEventsAfter(ctx, ch, identity, 0)
	return events, err
}

func relayReadEventsAfter(ctx context.Context, ch store.ChannelSubscription, identity store.ChannelIdentity, after int) ([]channelpkg.MemoryArtifactEvent, int, error) {
	if after < 0 {
		after = 0
	}
	var response relayEventsResponse
	err := relayDoSigned(ctx, http.MethodGet, ch, "/v1/channels/"+ch.ChannelID+"/events", fmt.Sprintf("after=%d", after), identity, nil, &response)
	return response.Events, response.Cursor, err
}

func relayAppendEvent(ctx context.Context, ch store.ChannelSubscription, identity store.ChannelIdentity, event channelpkg.MemoryArtifactEvent) (bool, error) {
	body, err := json.Marshal(event)
	if err != nil {
		return false, err
	}
	var response relayAppendResponse
	if err := relayDoSigned(ctx, http.MethodPost, ch, "/v1/channels/"+ch.ChannelID+"/events", "", identity, body, &response); err != nil {
		return false, err
	}
	return response.Duplicate, nil
}

func relayDoUnsigned(ctx context.Context, method, target string, body []byte, out any) error {
	var reader io.Reader
	if len(body) > 0 {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return err
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	return doRelayRequest(req, out)
}

func relayDoSigned(ctx context.Context, method string, ch store.ChannelSubscription, path, rawQuery string, identity store.ChannelIdentity, body []byte, out any) error {
	target := path
	if rawQuery != "" {
		target += "?" + rawQuery
	}
	endpoint := relayEndpoint(ch, target)
	var reader io.Reader
	if len(body) > 0 {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return err
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	timestamp := time.Now().UTC().Format(time.RFC3339)
	signature, err := channelpkg.SignRequest(method, target, timestamp, body, identity.PrivateKey)
	if err != nil {
		return err
	}
	req.Header.Set("X-Recoil-Node", identity.NodeID)
	req.Header.Set("X-Recoil-Timestamp", timestamp)
	req.Header.Set("X-Recoil-Signature", signature)
	return doRelayRequest(req, out)
}

func doRelayRequest(req *http.Request, out any) error {
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if readErr != nil {
		return readErr
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("relay %s %s failed: %s: %s", req.Method, req.URL.String(), resp.Status, strings.TrimSpace(string(body)))
	}
	if out == nil || len(strings.TrimSpace(string(body))) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode relay response: %w", err)
	}
	return nil
}

func relayEndpoint(ch store.ChannelSubscription, target string) string {
	return strings.TrimRight(ch.Path, "/") + target
}

func relayBaseURL(inviteURL string) (string, error) {
	u, err := url.Parse(inviteURL)
	if err != nil {
		return "", err
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("relay invite URL must be absolute")
	}
	u.RawQuery = ""
	u.Fragment = ""
	if idx := strings.Index(u.Path, "/v1/invites/"); idx >= 0 {
		u.Path = strings.TrimRight(u.Path[:idx], "/")
	} else {
		u.Path = ""
	}
	return strings.TrimRight(u.String(), "/"), nil
}
