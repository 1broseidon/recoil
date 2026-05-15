package store

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"
)

type ChannelIdentity struct {
	NodeID     string `json:"node_id"`
	PublicKey  string `json:"public_key"`
	PrivateKey string `json:"-"`
	CreatedAt  string `json:"created_at"`
}

type ChannelSubscription struct {
	ChannelID string `json:"channel_id"`
	Name      string `json:"name,omitempty"`
	Path      string `json:"path"`
	NodeID    string `json:"node_id"`
	Agent     string `json:"agent,omitempty"`
	ScopeKind string `json:"scope_kind"`
	ScopeID   string `json:"scope_id"`
	ProjectID string `json:"project_id,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	JoinedAt  string `json:"joined_at"`
	UpdatedAt string `json:"updated_at"`
}

type UpsertChannelSubscriptionParams struct {
	ChannelID string
	Name      string
	Path      string
	NodeID    string
	Agent     string
	ScopeKind string
	ScopeID   string
	ProjectID string
	SessionID string
}

type ChannelImportParams struct {
	EventID       string
	ChannelID     string
	ArtifactID    string
	PublisherNode string
	MemoryID      string
	SkippedReason string
}

type ChannelPeer struct {
	ChannelID string `json:"channel_id"`
	NodeID    string `json:"node_id"`
	Agent     string `json:"agent,omitempty"`
	LastSeen  string `json:"last_seen,omitempty"`
	SeenAt    string `json:"seen_at,omitempty"`
	Present   bool   `json:"present"`
}

type ChannelPeerDelta struct {
	NewPeers     []ChannelPeer `json:"new_peers,omitempty"`
	MissingPeers []ChannelPeer `json:"missing_peers,omitempty"`
}

type ChannelRefreshState struct {
	ChannelID       string `json:"channel_id"`
	EventCursor     int    `json:"event_cursor"`
	LastRefreshedAt string `json:"last_refreshed_at,omitempty"`
}

type ChannelOutboxEntry struct {
	MemoryID  string `json:"memory_id"`
	ChannelID string `json:"channel_id"`
	Reason    string `json:"reason,omitempty"`
	Status    string `json:"status"`
	Attempts  int    `json:"attempts"`
	LastError string `json:"last_error,omitempty"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func (s *Store) ensureChannelTables() error {
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS channel_identities (
			node_id TEXT PRIMARY KEY,
			public_key TEXT NOT NULL UNIQUE,
			private_key TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS channel_subscriptions (
			channel_id TEXT PRIMARY KEY,
			name TEXT,
			path TEXT NOT NULL,
			node_id TEXT NOT NULL,
			agent TEXT,
			scope_kind TEXT NOT NULL,
			scope_id TEXT NOT NULL,
			project_id TEXT,
			session_id TEXT,
			joined_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS channel_imports (
			event_id TEXT PRIMARY KEY,
			channel_id TEXT NOT NULL,
			artifact_id TEXT NOT NULL,
			publisher_node TEXT,
			memory_id TEXT,
			imported_at TEXT NOT NULL,
			skipped_reason TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS channel_peers (
			channel_id TEXT NOT NULL,
			node_id TEXT NOT NULL,
			agent TEXT,
			last_seen TEXT,
			seen_at TEXT NOT NULL,
			present INTEGER NOT NULL DEFAULT 1,
			PRIMARY KEY(channel_id, node_id)
		)`,
		`CREATE TABLE IF NOT EXISTS channel_refresh_state (
			channel_id TEXT PRIMARY KEY,
			event_cursor INTEGER NOT NULL DEFAULT 0,
			last_refreshed_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS channel_outbox (
			memory_id TEXT NOT NULL,
			channel_id TEXT NOT NULL,
			reason TEXT,
			status TEXT NOT NULL DEFAULT 'pending',
			attempts INTEGER NOT NULL DEFAULT 0,
			last_error TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY(memory_id, channel_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_channel_subscriptions_name ON channel_subscriptions(name)`,
		`CREATE INDEX IF NOT EXISTS idx_channel_imports_channel ON channel_imports(channel_id, artifact_id)`,
		`CREATE INDEX IF NOT EXISTS idx_channel_peers_channel ON channel_peers(channel_id, present)`,
		`CREATE INDEX IF NOT EXISTS idx_channel_outbox_status ON channel_outbox(status, updated_at)`,
	} {
		if _, err := s.db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) GetOrCreateChannelIdentity(ctx context.Context) (ChannelIdentity, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT node_id, public_key, private_key, created_at
		FROM channel_identities
		ORDER BY created_at ASC, node_id ASC
		LIMIT 1`)
	var id ChannelIdentity
	if err := row.Scan(&id.NodeID, &id.PublicKey, &id.PrivateKey, &id.CreatedAt); err == nil {
		return id, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return ChannelIdentity{}, err
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return ChannelIdentity{}, err
	}
	id = ChannelIdentity{
		NodeID:     channelNodeID(pub),
		PublicKey:  base64.StdEncoding.EncodeToString(pub),
		PrivateKey: base64.StdEncoding.EncodeToString(priv),
		CreatedAt:  time.Now().UTC().Format(time.RFC3339),
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO channel_identities (node_id, public_key, private_key, created_at)
		VALUES (?, ?, ?, ?)`,
		id.NodeID, id.PublicKey, id.PrivateKey, id.CreatedAt)
	if err != nil {
		return ChannelIdentity{}, err
	}
	return id, nil
}

func ChannelNodeIDForPublicKey(publicKey string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(publicKey))
	if err != nil {
		return "", err
	}
	if l := len(raw); l != ed25519.PublicKeySize {
		return "", fmt.Errorf("invalid public key size %d", l)
	}
	return channelNodeID(raw), nil
}

func channelNodeID(publicKey []byte) string {
	sum := sha256.Sum256(publicKey)
	return "node_" + hex.EncodeToString(sum[:])[:24]
}

func (s *Store) UpsertChannelSubscription(ctx context.Context, p UpsertChannelSubscriptionParams) (ChannelSubscription, error) {
	p.ChannelID = strings.TrimSpace(p.ChannelID)
	p.Name = strings.TrimSpace(p.Name)
	p.NodeID = strings.TrimSpace(p.NodeID)
	p.Agent = strings.TrimSpace(p.Agent)
	p.ScopeKind = strings.TrimSpace(p.ScopeKind)
	p.ScopeID = strings.TrimSpace(p.ScopeID)
	if p.ChannelID == "" {
		return ChannelSubscription{}, fmt.Errorf("channel id is required")
	}
	if p.Path == "" {
		return ChannelSubscription{}, fmt.Errorf("channel path is required")
	}
	target, err := normalizeChannelTarget(p.Path)
	if err != nil {
		return ChannelSubscription{}, err
	}
	if p.NodeID == "" {
		return ChannelSubscription{}, fmt.Errorf("node id is required")
	}
	if p.ScopeKind == "" || p.ScopeID == "" {
		return ChannelSubscription{}, fmt.Errorf("channel scope is required")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO channel_subscriptions (
			channel_id, name, path, node_id, agent, scope_kind, scope_id,
			project_id, session_id, joined_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(channel_id) DO UPDATE SET
			name = excluded.name,
			path = excluded.path,
			node_id = excluded.node_id,
			agent = excluded.agent,
			scope_kind = excluded.scope_kind,
			scope_id = excluded.scope_id,
			project_id = excluded.project_id,
			session_id = excluded.session_id,
			updated_at = excluded.updated_at`,
		p.ChannelID, emptyToNull(p.Name), target, p.NodeID, emptyToNull(p.Agent),
		p.ScopeKind, p.ScopeID, emptyToNull(p.ProjectID), emptyToNull(p.SessionID), now, now)
	if err != nil {
		return ChannelSubscription{}, err
	}
	return s.GetChannelSubscription(ctx, p.ChannelID)
}

func (s *Store) GetChannelSubscription(ctx context.Context, channelID string) (ChannelSubscription, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT channel_id, COALESCE(name, ''), path, node_id, COALESCE(agent, ''),
			scope_kind, scope_id, COALESCE(project_id, ''), COALESCE(session_id, ''),
			joined_at, updated_at
		FROM channel_subscriptions
		WHERE channel_id = ?`, strings.TrimSpace(channelID))
	return scanChannelSubscription(row)
}

func (s *Store) ResolveChannelSubscription(ctx context.Context, selector string) (ChannelSubscription, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		channels, err := s.ChannelSubscriptions(ctx)
		if err != nil {
			return ChannelSubscription{}, err
		}
		switch len(channels) {
		case 0:
			return ChannelSubscription{}, fmt.Errorf("no channels joined")
		case 1:
			return channels[0], nil
		default:
			return ChannelSubscription{}, fmt.Errorf("multiple channels joined; pass --channel")
		}
	}
	args := []any{selector, selector}
	pathSelector, _ := normalizeChannelTarget(selector)
	args = append(args, pathSelector)
	rows, err := s.db.QueryContext(ctx, `
		SELECT channel_id, COALESCE(name, ''), path, node_id, COALESCE(agent, ''),
			scope_kind, scope_id, COALESCE(project_id, ''), COALESCE(session_id, ''),
			joined_at, updated_at
		FROM channel_subscriptions
		WHERE channel_id = ? OR COALESCE(name, '') = ? OR path = ?
		ORDER BY joined_at ASC, channel_id ASC
		LIMIT 2`, args...)
	if err != nil {
		return ChannelSubscription{}, err
	}
	defer rows.Close()
	var channels []ChannelSubscription
	for rows.Next() {
		ch, err := scanChannelSubscription(rows)
		if err != nil {
			return ChannelSubscription{}, err
		}
		channels = append(channels, ch)
	}
	if err := rows.Err(); err != nil {
		return ChannelSubscription{}, err
	}
	if len(channels) == 0 {
		return ChannelSubscription{}, fmt.Errorf("channel %q is not joined", selector)
	}
	if len(channels) > 1 {
		return ChannelSubscription{}, fmt.Errorf("channel selector %q is ambiguous", selector)
	}
	return channels[0], nil
}

func normalizeChannelTarget(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("channel target is required")
	}
	if u, err := url.Parse(value); err == nil && u.Scheme != "" && u.Host != "" {
		return value, nil
	}
	return filepath.Abs(value)
}

func (s *Store) ChannelSubscriptions(ctx context.Context) ([]ChannelSubscription, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT channel_id, COALESCE(name, ''), path, node_id, COALESCE(agent, ''),
			scope_kind, scope_id, COALESCE(project_id, ''), COALESCE(session_id, ''),
			joined_at, updated_at
		FROM channel_subscriptions
		ORDER BY updated_at DESC, channel_id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var channels []ChannelSubscription
	for rows.Next() {
		ch, err := scanChannelSubscription(rows)
		if err != nil {
			return nil, err
		}
		channels = append(channels, ch)
	}
	return channels, rows.Err()
}

func scanChannelSubscription(scanner memoryScanner) (ChannelSubscription, error) {
	var ch ChannelSubscription
	err := scanner.Scan(
		&ch.ChannelID, &ch.Name, &ch.Path, &ch.NodeID, &ch.Agent,
		&ch.ScopeKind, &ch.ScopeID, &ch.ProjectID, &ch.SessionID,
		&ch.JoinedAt, &ch.UpdatedAt,
	)
	return ch, err
}

func (s *Store) HasChannelImport(ctx context.Context, eventID string) (bool, error) {
	var found int
	err := s.db.QueryRowContext(ctx,
		`SELECT 1 FROM channel_imports WHERE event_id = ?`,
		strings.TrimSpace(eventID),
	).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (s *Store) RecordChannelImport(ctx context.Context, p ChannelImportParams) error {
	p.EventID = strings.TrimSpace(p.EventID)
	p.ChannelID = strings.TrimSpace(p.ChannelID)
	p.ArtifactID = strings.TrimSpace(p.ArtifactID)
	if p.EventID == "" || p.ChannelID == "" || p.ArtifactID == "" {
		return fmt.Errorf("event id, channel id, and artifact id are required")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO channel_imports (
			event_id, channel_id, artifact_id, publisher_node,
			memory_id, imported_at, skipped_reason
		) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(event_id) DO UPDATE SET
			memory_id = COALESCE(excluded.memory_id, channel_imports.memory_id),
			skipped_reason = excluded.skipped_reason`,
		p.EventID, p.ChannelID, p.ArtifactID, emptyToNull(strings.TrimSpace(p.PublisherNode)),
		emptyToNull(strings.TrimSpace(p.MemoryID)), now, emptyToNull(strings.TrimSpace(p.SkippedReason)))
	return err
}

func (s *Store) RecordChannelPeers(ctx context.Context, channelID string, peers []ChannelPeer) (ChannelPeerDelta, error) {
	channelID = strings.TrimSpace(channelID)
	if channelID == "" {
		return ChannelPeerDelta{}, fmt.Errorf("channel id is required")
	}
	existing, err := s.channelPeers(ctx, channelID, true)
	if err != nil {
		return ChannelPeerDelta{}, err
	}
	existingByNode := map[string]ChannelPeer{}
	for _, peer := range existing {
		existingByNode[peer.NodeID] = peer
	}

	now := time.Now().UTC().Format(time.RFC3339)
	current := map[string]ChannelPeer{}
	delta := ChannelPeerDelta{}
	for _, peer := range peers {
		peer.ChannelID = channelID
		peer.NodeID = strings.TrimSpace(peer.NodeID)
		peer.Agent = strings.TrimSpace(peer.Agent)
		peer.LastSeen = strings.TrimSpace(peer.LastSeen)
		if peer.NodeID == "" {
			continue
		}
		peer.SeenAt = now
		peer.Present = true
		current[peer.NodeID] = peer
		if _, found := existingByNode[peer.NodeID]; !found {
			delta.NewPeers = append(delta.NewPeers, peer)
		}
	}
	return s.recordRemainingChannelPeers(ctx, channelID, existingByNode, current, delta)
}

func (s *Store) recordRemainingChannelPeers(ctx context.Context, channelID string, existingByNode, current map[string]ChannelPeer, delta ChannelPeerDelta) (ChannelPeerDelta, error) {
	for _, peer := range current {
		if _, err := s.db.ExecContext(ctx, `
			INSERT INTO channel_peers (channel_id, node_id, agent, last_seen, seen_at, present)
			VALUES (?, ?, ?, ?, ?, 1)
			ON CONFLICT(channel_id, node_id) DO UPDATE SET
				agent = excluded.agent,
				last_seen = excluded.last_seen,
				seen_at = excluded.seen_at,
				present = 1`,
			peer.ChannelID, peer.NodeID, emptyToNull(peer.Agent), emptyToNull(peer.LastSeen), peer.SeenAt); err != nil {
			return ChannelPeerDelta{}, err
		}
	}
	for nodeID, peer := range existingByNode {
		if _, found := current[nodeID]; found {
			continue
		}
		peer.Present = false
		delta.MissingPeers = append(delta.MissingPeers, peer)
		if _, err := s.db.ExecContext(ctx, `
			UPDATE channel_peers
			SET present = 0, seen_at = ?
			WHERE channel_id = ? AND node_id = ?`,
			time.Now().UTC().Format(time.RFC3339), channelID, nodeID); err != nil {
			return ChannelPeerDelta{}, err
		}
	}
	return delta, nil
}

func (s *Store) channelPeers(ctx context.Context, channelID string, presentOnly bool) ([]ChannelPeer, error) {
	query := `
		SELECT channel_id, node_id, COALESCE(agent, ''), COALESCE(last_seen, ''), seen_at, present
		FROM channel_peers
		WHERE channel_id = ?`
	if presentOnly {
		query += ` AND present = 1`
	}
	rows, err := s.db.QueryContext(ctx, query, strings.TrimSpace(channelID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var peers []ChannelPeer
	for rows.Next() {
		var peer ChannelPeer
		var present int
		if err := rows.Scan(&peer.ChannelID, &peer.NodeID, &peer.Agent, &peer.LastSeen, &peer.SeenAt, &present); err != nil {
			return nil, err
		}
		peer.Present = present == 1
		peers = append(peers, peer)
	}
	return peers, rows.Err()
}

func (s *Store) PresentChannelPeers(ctx context.Context, channelID string) ([]ChannelPeer, error) {
	return s.channelPeers(ctx, channelID, true)
}

func (s *Store) ChannelRefreshState(ctx context.Context, channelID string) (ChannelRefreshState, error) {
	channelID = strings.TrimSpace(channelID)
	row := s.db.QueryRowContext(ctx, `
		SELECT channel_id, event_cursor, last_refreshed_at
		FROM channel_refresh_state
		WHERE channel_id = ?`, channelID)
	var state ChannelRefreshState
	if err := row.Scan(&state.ChannelID, &state.EventCursor, &state.LastRefreshedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ChannelRefreshState{ChannelID: channelID}, nil
		}
		return ChannelRefreshState{}, err
	}
	return state, nil
}

func (s *Store) RecordChannelRefresh(ctx context.Context, channelID string, eventCursor int) error {
	channelID = strings.TrimSpace(channelID)
	if channelID == "" {
		return fmt.Errorf("channel id is required")
	}
	if eventCursor < 0 {
		eventCursor = 0
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO channel_refresh_state (channel_id, event_cursor, last_refreshed_at)
		VALUES (?, ?, ?)
		ON CONFLICT(channel_id) DO UPDATE SET
			event_cursor = excluded.event_cursor,
			last_refreshed_at = excluded.last_refreshed_at`,
		channelID, eventCursor, now)
	return err
}

func (s *Store) EnqueueChannelOutbox(ctx context.Context, memoryID, channelID, reason string) error {
	memoryID = strings.TrimSpace(memoryID)
	channelID = strings.TrimSpace(channelID)
	if memoryID == "" || channelID == "" {
		return fmt.Errorf("memory id and channel id are required")
	}
	var status string
	err := s.db.QueryRowContext(ctx, `
		SELECT status
		FROM channel_outbox
		WHERE memory_id = ? AND channel_id = ?`, memoryID, channelID).Scan(&status)
	if err == nil && status == "published" {
		return nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO channel_outbox (
			memory_id, channel_id, reason, status, attempts, last_error, created_at, updated_at
		) VALUES (?, ?, ?, 'pending', 0, NULL, ?, ?)
		ON CONFLICT(memory_id, channel_id) DO UPDATE SET
			reason = excluded.reason,
			status = 'pending',
			updated_at = excluded.updated_at`,
		memoryID, channelID, emptyToNull(strings.TrimSpace(reason)), now, now)
	return err
}

func (s *Store) ChannelOutboxEntries(ctx context.Context, channelID string, includePublished bool, limit int) ([]ChannelOutboxEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	where := `WHERE 1=1`
	args := []any{}
	if strings.TrimSpace(channelID) != "" {
		where += ` AND channel_id = ?`
		args = append(args, strings.TrimSpace(channelID))
	}
	if !includePublished {
		where += ` AND status != 'published'`
	}
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, `
		SELECT memory_id, channel_id, COALESCE(reason, ''), status, attempts,
			COALESCE(last_error, ''), created_at, updated_at
		FROM channel_outbox
		`+where+`
		ORDER BY updated_at ASC, memory_id ASC
		LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []ChannelOutboxEntry
	for rows.Next() {
		var entry ChannelOutboxEntry
		if err := rows.Scan(
			&entry.MemoryID, &entry.ChannelID, &entry.Reason, &entry.Status,
			&entry.Attempts, &entry.LastError, &entry.CreatedAt, &entry.UpdatedAt,
		); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (s *Store) PendingChannelOutbox(ctx context.Context, channelID string, limit int) ([]ChannelOutboxEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	where := `WHERE status = 'pending'`
	args := []any{}
	if strings.TrimSpace(channelID) != "" {
		where += ` AND channel_id = ?`
		args = append(args, strings.TrimSpace(channelID))
	}
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, `
		SELECT memory_id, channel_id, COALESCE(reason, ''), status, attempts,
			COALESCE(last_error, ''), created_at, updated_at
		FROM channel_outbox
		`+where+`
		ORDER BY updated_at ASC, memory_id ASC
		LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []ChannelOutboxEntry
	for rows.Next() {
		var entry ChannelOutboxEntry
		if err := rows.Scan(
			&entry.MemoryID, &entry.ChannelID, &entry.Reason, &entry.Status,
			&entry.Attempts, &entry.LastError, &entry.CreatedAt, &entry.UpdatedAt,
		); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (s *Store) MarkChannelOutboxPublished(ctx context.Context, memoryID, channelID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
		UPDATE channel_outbox
		SET status = 'published', last_error = NULL, updated_at = ?
		WHERE memory_id = ? AND channel_id = ?`,
		now, strings.TrimSpace(memoryID), strings.TrimSpace(channelID))
	return err
}

func (s *Store) RecordChannelOutboxFailure(ctx context.Context, memoryID, channelID, lastError string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
		UPDATE channel_outbox
		SET status = 'pending', attempts = attempts + 1, last_error = ?, updated_at = ?
		WHERE memory_id = ? AND channel_id = ?`,
		strings.TrimSpace(lastError), now, strings.TrimSpace(memoryID), strings.TrimSpace(channelID))
	return err
}
