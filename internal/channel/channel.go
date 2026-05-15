package channel

import (
	"bufio"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/1broseidon/recoil/internal/store"
)

var (
	appendLocksMu sync.Mutex
	appendLocks   = map[string]*sync.Mutex{}
)

func appendLockFor(path string) *sync.Mutex {
	key := filepath.Clean(path)
	appendLocksMu.Lock()
	defer appendLocksMu.Unlock()
	m, ok := appendLocks[key]
	if !ok {
		m = &sync.Mutex{}
		appendLocks[key] = m
	}
	return m
}

const (
	Version             = "0.1"
	ManifestKind        = "recoil.channel.v1"
	RosterCardKind      = "recoil.channel_roster_card.v1"
	MemoryArtifactKind  = "recoil.memory_artifact.v1"
	EventsFilename      = "events.jsonl"
	ManifestFilename    = "channel.json"
	RosterDirectoryName = "roster"
)

type ChannelManifest struct {
	Version   string `json:"version"`
	Kind      string `json:"kind"`
	ChannelID string `json:"channel_id"`
	Name      string `json:"name,omitempty"`
	CreatedAt string `json:"created_at"`
}

type ScopeRef struct {
	Kind      string `json:"kind"`
	ID        string `json:"id"`
	ProjectID string `json:"project_id,omitempty"`
	SessionID string `json:"session_id,omitempty"`
}

type RosterCard struct {
	Version      string     `json:"version"`
	Kind         string     `json:"kind"`
	ChannelID    string     `json:"channel_id"`
	NodeID       string     `json:"node_id"`
	Agent        string     `json:"agent,omitempty"`
	PublicKey    string     `json:"public_key"`
	Scopes       []ScopeRef `json:"scopes,omitempty"`
	Capabilities []string   `json:"capabilities,omitempty"`
	JoinedAt     string     `json:"joined_at"`
	LastSeen     string     `json:"last_seen"`
	Signature    string     `json:"signature,omitempty"`
}

type Publisher struct {
	NodeID    string `json:"node_id"`
	Agent     string `json:"agent,omitempty"`
	PublicKey string `json:"public_key"`
}

type MemoryArtifactEvent struct {
	Version          string    `json:"version"`
	Kind             string    `json:"kind"`
	ChannelID        string    `json:"channel_id"`
	EventID          string    `json:"event_id"`
	ArtifactID       string    `json:"artifact_id"`
	Op               string    `json:"op"`
	Publisher        Publisher `json:"publisher"`
	Scope            ScopeRef  `json:"scope"`
	Role             string    `json:"role,omitempty"`
	Content          string    `json:"content"`
	ContentHash      string    `json:"content_hash"`
	PayloadHash      string    `json:"payload_hash"`
	SourceKind       string    `json:"source_kind,omitempty"`
	SourceAgent      string    `json:"source_agent,omitempty"`
	SourcePath       string    `json:"source_path,omitempty"`
	SourceRef        string    `json:"source_ref,omitempty"`
	SourceMemoryID   string    `json:"source_memory_id"`
	SourceMemoryHash string    `json:"source_memory_hash,omitempty"`
	MetadataJSON     string    `json:"metadata_json,omitempty"`
	Validity         string    `json:"validity,omitempty"`
	ClaimKey         string    `json:"claim_key,omitempty"`
	Supersedes       string    `json:"supersedes,omitempty"`
	SupersededBy     string    `json:"superseded_by,omitempty"`
	Subjects         []string  `json:"subjects,omitempty"`
	CreatedAt        string    `json:"created_at"`
	PublishedAt      string    `json:"published_at"`
	Signature        string    `json:"signature,omitempty"`
}

type ArtifactSummary struct {
	EventID     string   `json:"event_id"`
	ArtifactID  string   `json:"artifact_id"`
	Publisher   string   `json:"publisher"`
	Agent       string   `json:"agent,omitempty"`
	Role        string   `json:"role,omitempty"`
	ClaimKey    string   `json:"claim_key,omitempty"`
	Validity    string   `json:"validity,omitempty"`
	Subjects    []string `json:"subjects,omitempty"`
	PublishedAt string   `json:"published_at"`
	Summary     string   `json:"summary"`
}

func EnsureChannel(path, name string) (ChannelManifest, string, error) {
	absPath, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return ChannelManifest{}, "", err
	}
	if absPath == "" {
		return ChannelManifest{}, "", fmt.Errorf("channel path is required")
	}
	if err := os.MkdirAll(filepath.Join(absPath, RosterDirectoryName), 0o700); err != nil {
		return ChannelManifest{}, "", err
	}
	if _, err := os.OpenFile(filepath.Join(absPath, EventsFilename), os.O_CREATE, 0o600); err != nil {
		return ChannelManifest{}, "", err
	}
	manifest, err := LoadManifest(absPath)
	if err == nil {
		return manifest, absPath, nil
	}
	if !os.IsNotExist(err) {
		return ChannelManifest{}, "", err
	}
	manifest = ChannelManifest{
		Version:   Version,
		Kind:      ManifestKind,
		ChannelID: randomID("ch"),
		Name:      strings.TrimSpace(name),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if manifest.Name == "" {
		manifest.Name = filepath.Base(absPath)
	}
	if err := writeJSONFile(filepath.Join(absPath, ManifestFilename), manifest); err != nil {
		return ChannelManifest{}, "", err
	}
	return manifest, absPath, nil
}

func LoadManifest(path string) (ChannelManifest, error) {
	var manifest ChannelManifest
	data, err := os.ReadFile(filepath.Join(path, ManifestFilename))
	if err != nil {
		return manifest, err
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return manifest, err
	}
	if manifest.Kind != ManifestKind || strings.TrimSpace(manifest.ChannelID) == "" {
		return manifest, fmt.Errorf("invalid channel manifest at %s", filepath.Join(path, ManifestFilename))
	}
	return manifest, nil
}

func BuildRosterCard(ch store.ChannelSubscription, id store.ChannelIdentity) RosterCard {
	now := time.Now().UTC().Format(time.RFC3339)
	return RosterCard{
		Version:   Version,
		Kind:      RosterCardKind,
		ChannelID: ch.ChannelID,
		NodeID:    id.NodeID,
		Agent:     ch.Agent,
		PublicKey: id.PublicKey,
		Scopes: []ScopeRef{{
			Kind:      ch.ScopeKind,
			ID:        ch.ScopeID,
			ProjectID: ch.ProjectID,
			SessionID: ch.SessionID,
		}},
		Capabilities: []string{"publish", "subscribe", "replay"},
		JoinedAt:     firstNonEmpty(ch.JoinedAt, now),
		LastSeen:     now,
	}
}

func WriteRosterCard(path string, card RosterCard, privateKey string) error {
	if err := SignRosterCard(&card, privateKey); err != nil {
		return err
	}
	return WriteSignedRosterCard(path, card)
}

func WriteSignedRosterCard(path string, card RosterCard) error {
	if err := VerifyRosterCard(card); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(path, RosterDirectoryName), 0o700); err != nil {
		return err
	}
	return writeJSONFile(filepath.Join(path, RosterDirectoryName, card.NodeID+".json"), card)
}

func ReadRoster(path string) ([]RosterCard, error) {
	entries, err := os.ReadDir(filepath.Join(path, RosterDirectoryName))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var cards []RosterCard
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		var card RosterCard
		data, err := os.ReadFile(filepath.Join(path, RosterDirectoryName, entry.Name()))
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(data, &card); err != nil {
			return nil, err
		}
		if err := VerifyRosterCard(card); err != nil {
			continue
		}
		cards = append(cards, card)
	}
	sort.SliceStable(cards, func(i, j int) bool {
		if cards[i].LastSeen == cards[j].LastSeen {
			return cards[i].NodeID < cards[j].NodeID
		}
		return cards[i].LastSeen > cards[j].LastSeen
	})
	return cards, nil
}

func NewMemoryArtifactEvent(ch store.ChannelSubscription, id store.ChannelIdentity, mem store.Memory) MemoryArtifactEvent {
	event := MemoryArtifactEvent{
		Version:    Version,
		Kind:       MemoryArtifactKind,
		ChannelID:  ch.ChannelID,
		Op:         "assert",
		ArtifactID: artifactID(ch.ChannelID, id.NodeID, mem),
		Publisher: Publisher{
			NodeID:    id.NodeID,
			Agent:     firstNonEmpty(ch.Agent, mem.SourceAgent),
			PublicKey: id.PublicKey,
		},
		Scope: ScopeRef{
			Kind:      mem.ScopeKind,
			ID:        mem.ScopeID,
			ProjectID: mem.ProjectID,
			SessionID: mem.SessionID,
		},
		Role:             mem.Role,
		Content:          mem.Content,
		ContentHash:      hashParts(mem.Content),
		SourceKind:       mem.SourceKind,
		SourceAgent:      mem.SourceAgent,
		SourcePath:       mem.SourcePath,
		SourceRef:        mem.SourceRef,
		SourceMemoryID:   mem.ID,
		SourceMemoryHash: mem.Hash,
		MetadataJSON:     mem.MetadataJSON,
		Validity:         mem.Validity,
		ClaimKey:         mem.ClaimKey,
		Supersedes:       mem.Supersedes,
		SupersededBy:     mem.SupersededBy,
		Subjects:         subjectsFromMemory(mem),
		CreatedAt:        mem.CreatedAt,
		PublishedAt:      time.Now().UTC().Format(time.RFC3339),
	}
	event.PayloadHash = hashParts(event.Content, event.MetadataJSON, event.Validity, event.ClaimKey)
	return event
}

func AppendEventIfMissing(path string, event MemoryArtifactEvent) (bool, error) {
	lock := appendLockFor(path)
	lock.Lock()
	defer lock.Unlock()
	events, err := ReadEvents(path)
	if err != nil {
		return false, err
	}
	for _, existing := range events {
		if existing.Publisher.NodeID == event.Publisher.NodeID && existing.ArtifactID == event.ArtifactID {
			return true, nil
		}
	}
	f, err := os.OpenFile(filepath.Join(path, EventsFilename), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return false, err
	}
	defer f.Close()
	data, err := json.Marshal(event)
	if err != nil {
		return false, err
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		return false, err
	}
	return false, nil
}

func ReadEvents(path string) ([]MemoryArtifactEvent, error) {
	f, err := os.Open(filepath.Join(path, EventsFilename))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var events []MemoryArtifactEvent
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event MemoryArtifactEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, scanner.Err()
}

func ArtifactSummaries(events []MemoryArtifactEvent) []ArtifactSummary {
	summaries := make([]ArtifactSummary, 0, len(events))
	for _, event := range events {
		summaries = append(summaries, ArtifactSummary{
			EventID:     event.EventID,
			ArtifactID:  event.ArtifactID,
			Publisher:   event.Publisher.NodeID,
			Agent:       event.Publisher.Agent,
			Role:        event.Role,
			ClaimKey:    event.ClaimKey,
			Validity:    event.Validity,
			Subjects:    event.Subjects,
			PublishedAt: event.PublishedAt,
			Summary:     truncateOneLine(event.Content, 120),
		})
	}
	sort.SliceStable(summaries, func(i, j int) bool {
		if summaries[i].PublishedAt == summaries[j].PublishedAt {
			return summaries[i].ArtifactID < summaries[j].ArtifactID
		}
		return summaries[i].PublishedAt > summaries[j].PublishedAt
	})
	return summaries
}

func SignMemoryArtifactEvent(event *MemoryArtifactEvent, privateKey string) error {
	body, err := signingBytes(*event)
	if err != nil {
		return err
	}
	event.EventID = "evt_" + hashBytes(body)[:32]
	sig, err := sign(body, privateKey)
	if err != nil {
		return err
	}
	event.Signature = sig
	return nil
}

func VerifyMemoryArtifactEvent(event MemoryArtifactEvent) error {
	if event.Kind != MemoryArtifactKind {
		return fmt.Errorf("unsupported event kind %q", event.Kind)
	}
	if event.Publisher.PublicKey == "" || event.Publisher.NodeID == "" {
		return fmt.Errorf("event publisher identity is incomplete")
	}
	nodeID, err := store.ChannelNodeIDForPublicKey(event.Publisher.PublicKey)
	if err != nil {
		return err
	}
	if nodeID != event.Publisher.NodeID {
		return fmt.Errorf("event publisher node id does not match public key")
	}
	body, err := signingBytes(event)
	if err != nil {
		return err
	}
	if want := "evt_" + hashBytes(body)[:32]; event.EventID != want {
		return fmt.Errorf("event id hash mismatch")
	}
	return verify(body, event.Publisher.PublicKey, event.Signature)
}

func SignRosterCard(card *RosterCard, privateKey string) error {
	body, err := signingBytes(*card)
	if err != nil {
		return err
	}
	sig, err := sign(body, privateKey)
	if err != nil {
		return err
	}
	card.Signature = sig
	return nil
}

func VerifyRosterCard(card RosterCard) error {
	if card.Kind != RosterCardKind {
		return fmt.Errorf("unsupported roster card kind %q", card.Kind)
	}
	nodeID, err := store.ChannelNodeIDForPublicKey(card.PublicKey)
	if err != nil {
		return err
	}
	if nodeID != card.NodeID {
		return fmt.Errorf("roster node id does not match public key")
	}
	body, err := signingBytes(card)
	if err != nil {
		return err
	}
	return verify(body, card.PublicKey, card.Signature)
}

func SourcePathForEvent(event MemoryArtifactEvent) string {
	return "channel://" + event.ChannelID + "/" + event.Publisher.NodeID + "/" + event.ArtifactID
}

func SignRequest(method, target, timestamp string, body []byte, privateKey string) (string, error) {
	return sign(requestSigningBytes(method, target, timestamp, body), privateKey)
}

func VerifyRequest(method, target, timestamp string, body []byte, publicKey, signature string) error {
	return verify(requestSigningBytes(method, target, timestamp, body), publicKey, signature)
}

func RequestBodyHash(body []byte) string {
	return hashBytes(body)
}

func requestSigningBytes(method, target, timestamp string, body []byte) []byte {
	return []byte(strings.ToUpper(strings.TrimSpace(method)) + "\n" +
		strings.TrimSpace(target) + "\n" +
		strings.TrimSpace(timestamp) + "\n" +
		RequestBodyHash(body))
}

func signingBytes(v any) ([]byte, error) {
	switch typed := v.(type) {
	case MemoryArtifactEvent:
		typed.EventID = ""
		typed.Signature = ""
		return json.Marshal(typed)
	case RosterCard:
		typed.Signature = ""
		return json.Marshal(typed)
	default:
		return nil, fmt.Errorf("unsupported signing value %T", v)
	}
}

func sign(body []byte, privateKey string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(privateKey))
	if err != nil {
		return "", err
	}
	if l := len(raw); l != ed25519.PrivateKeySize {
		return "", fmt.Errorf("invalid private key size %d", l)
	}
	return base64.StdEncoding.EncodeToString(ed25519.Sign(ed25519.PrivateKey(raw), body)), nil
}

func verify(body []byte, publicKey, signature string) error {
	pub, err := base64.StdEncoding.DecodeString(strings.TrimSpace(publicKey))
	if err != nil {
		return err
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(signature))
	if err != nil {
		return err
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), body, sig) {
		return fmt.Errorf("signature verification failed")
	}
	return nil
}

func artifactID(channelID, nodeID string, mem store.Memory) string {
	return "art_" + hashParts(channelID, nodeID, mem.ID, mem.Hash, mem.Role, mem.Content, mem.ClaimKey)[:24]
}

func randomID(prefix string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return prefix + "_" + hashParts(time.Now().UTC().Format(time.RFC3339Nano))[:24]
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}

func hashParts(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func hashBytes(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func writeJSONFile(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}

func subjectsFromMemory(mem store.Memory) []string {
	seen := map[string]bool{}
	var subjects []string
	add := func(value string) {
		for _, part := range strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
			return r == '.' || r == '-' || r == '_' || r == '/' || r == ':' || r == ' '
		}) {
			part = strings.TrimSpace(part)
			if len(part) < 3 || seen[part] {
				continue
			}
			seen[part] = true
			subjects = append(subjects, part)
		}
	}
	add(mem.ClaimKey)
	add(mem.Role)
	add(mem.SourcePath)
	sort.Strings(subjects)
	if len(subjects) > 12 {
		subjects = subjects[:12]
	}
	return subjects
}

func truncateOneLine(value string, max int) string {
	value = strings.Join(strings.Fields(value), " ")
	if max <= 0 || len(value) <= max {
		return value
	}
	if max <= 3 {
		return value[:max]
	}
	return strings.TrimSpace(value[:max-3]) + "..."
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
