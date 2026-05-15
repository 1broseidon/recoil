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
		`CREATE INDEX IF NOT EXISTS idx_channel_subscriptions_name ON channel_subscriptions(name)`,
		`CREATE INDEX IF NOT EXISTS idx_channel_imports_channel ON channel_imports(channel_id, artifact_id)`,
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
