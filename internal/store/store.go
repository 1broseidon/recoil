package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/1broseidon/recoil/internal/redact"
	"github.com/1broseidon/recoil/internal/retrieval"
	"github.com/1broseidon/recoil/internal/sourcequality"
	_ "github.com/mattn/go-sqlite3"
)

var (
	ErrNotFound  = errors.New("memory not found")
	ErrAmbiguous = errors.New("memory ID prefix is ambiguous")
)

type Store struct {
	db   *sql.DB
	path string
}

type Memory struct {
	ID           string  `json:"id"`
	Hash         string  `json:"hash,omitempty"`
	Role         string  `json:"role,omitempty"`
	Content      string  `json:"content"`
	SourceKind   string  `json:"source_kind,omitempty"`
	SourceAgent  string  `json:"source_agent,omitempty"`
	SourcePath   string  `json:"source_path,omitempty"`
	SourceRef    string  `json:"source_ref,omitempty"`
	ScopeKind    string  `json:"scope_kind"`
	ScopeID      string  `json:"scope_id"`
	ProjectID    string  `json:"project_id,omitempty"`
	SessionID    string  `json:"session_id,omitempty"`
	Room         string  `json:"room,omitempty"`
	MetadataJSON string  `json:"metadata_json,omitempty"`
	Validity     string  `json:"validity"`
	ClaimKey     string  `json:"claim_key,omitempty"`
	Supersedes   string  `json:"supersedes,omitempty"`
	SupersededBy string  `json:"superseded_by,omitempty"`
	CreatedAt    string  `json:"created_at"`
	TombstonedAt string  `json:"tombstoned_at,omitempty"`
	Score        float64 `json:"score,omitempty"`
	Excerpt      string  `json:"excerpt,omitempty"`
	Why          string  `json:"why,omitempty"`
}

type AddMemoryParams struct {
	Role         string
	Content      string
	SourceKind   string
	SourceAgent  string
	SourcePath   string
	SourceRef    string
	ScopeKind    string
	ScopeID      string
	ProjectID    string
	SessionID    string
	Room         string
	MetadataJSON string
	Validity     string
	ClaimKey     string
	Supersedes   string
	SupersededBy string
	CreatedAt    string
}

type SearchParams struct {
	Query         string
	ScopeKind     string
	ScopeID       string
	SourceKind    string
	SourceAgent   string
	SourcePath    string
	Role          string
	ClaimKey      string
	Validity      string
	Since         string
	Before        string
	Limit         int
	Lifecycle     string
	QueryDate     string
	SignalRerank  bool
	SourceQuality sourcequality.Options
}

type ListParams struct {
	ScopeKind      string
	ScopeID        string
	SourceKind     string
	SourceAgent    string
	SourcePath     string
	Role           string
	ClaimKey       string
	Validity       string
	Since          string
	Before         string
	Limit          int
	IncludeDeleted bool
	Lifecycle      string
}

type EmbeddingRecord struct {
	MemoryID    string    `json:"memory_id"`
	Provider    string    `json:"provider"`
	Model       string    `json:"model"`
	Dims        int       `json:"dims"`
	Vector      []float64 `json:"vector,omitempty"`
	ContentHash string    `json:"content_hash"`
	CreatedAt   string    `json:"created_at"`
}

type SemanticSearchParams struct {
	QueryVector []float64
	Provider    string
	Model       string
	ScopeKind   string
	ScopeID     string
	SourceKind  string
	SourceAgent string
	SourcePath  string
	Role        string
	ClaimKey    string
	Validity    string
	Since       string
	Before      string
	Limit       int
	Lifecycle   string
}

const (
	LifecycleAny        = ""
	LifecycleCurrent    = "current"
	LifecycleHistorical = "historical"
)

type ForgetParams struct {
	IDOrPrefix string
	Reason     string
	Destroy    bool
}

type LifecycleParams struct {
	IDOrPrefix   string
	Validity     string
	ClaimKey     string
	Supersedes   string
	SupersededBy string
}

type MetadataParams struct {
	IDOrPrefix   string
	MetadataJSON string
}

type ForgetResult struct {
	Memory    Memory `json:"memory"`
	Destroyed bool   `json:"destroyed"`
}

type BulkForgetResult struct {
	IDs       []string `json:"ids"`
	Count     int      `json:"count"`
	Destroyed bool     `json:"destroyed"`
}

type SourceRefreshParams struct {
	Kind            string
	Path            string
	Agent           string
	ScopeKind       string
	ScopeID         string
	Role            string
	ContentHash     string
	ModTime         string
	Size            int64
	ChunkCount      int
	MetadataJSON    string
	ActiveMemoryIDs []string
}

type SourceRefreshResult struct {
	ID      string
	Changed bool
	Staled  int
}

type FileSource struct {
	ID          string `json:"id"`
	Path        string `json:"path"`
	Agent       string `json:"agent,omitempty"`
	ScopeKind   string `json:"scope_kind"`
	ScopeID     string `json:"scope_id"`
	ContentHash string `json:"content_hash,omitempty"`
	ModTime     string `json:"mod_time,omitempty"`
	SizeBytes   int64  `json:"size_bytes,omitempty"`
	ChunkCount  int    `json:"chunk_count,omitempty"`
	LastMinedAt string `json:"last_mined_at,omitempty"`
	DeletedAt   string `json:"deleted_at,omitempty"`
}

type Counts struct {
	Total      int
	Active     int
	Tombstoned int
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	_ = os.Chmod(filepath.Dir(path), 0o700)

	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_synchronous=NORMAL&_busy_timeout=5000&_foreign_keys=ON")
	if err != nil {
		return nil, err
	}
	st := &Store{db: db, path: path}
	if err := st.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	st.tune()
	tightenStorePermissions(path)
	return st, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	_, checkpointErr := s.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	closeErr := s.db.Close()
	if closeErr != nil {
		return closeErr
	}
	return checkpointErr
}

func (s *Store) Path() string {
	return s.path
}

func (s *Store) Backup(dest string) error {
	dest = strings.TrimSpace(dest)
	if dest == "" {
		return fmt.Errorf("backup destination is required")
	}
	if dest == s.path {
		return fmt.Errorf("backup destination must differ from source")
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return err
	}
	if _, err := s.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		return err
	}
	if err := os.Remove(dest); err != nil && !os.IsNotExist(err) {
		return err
	}
	if _, err := s.db.Exec("VACUUM INTO ?", dest); err != nil {
		return err
	}
	tightenStorePermissions(dest)
	return nil
}

func (s *Store) AddMemory(ctx context.Context, p AddMemoryParams) (*Memory, bool, error) {
	p.Content = redact.Content(p.Content)
	p.SourceKind = normalizeSourceKind(p.SourceKind)
	p.ScopeKind = strings.TrimSpace(p.ScopeKind)
	p.ScopeID = strings.TrimSpace(p.ScopeID)
	if strings.TrimSpace(p.Content) == "" {
		return nil, false, fmt.Errorf("memory content is empty")
	}
	if p.ScopeKind == "" || p.ScopeID == "" {
		return nil, false, fmt.Errorf("memory scope is required")
	}
	lifecycle, err := lifecycleFromAddParams(p)
	if err != nil {
		return nil, false, err
	}

	hash := memoryHash(p)
	id := publicID(hash)
	createdAt := strings.TrimSpace(p.CreatedAt)
	if createdAt == "" {
		createdAt = time.Now().UTC().Format(time.RFC3339)
	} else if _, err := time.Parse(time.RFC3339, createdAt); err != nil {
		return nil, false, fmt.Errorf("invalid created_at: %w", err)
	}

	res, err := s.db.ExecContext(ctx, `
			INSERT OR IGNORE INTO memories (
				id, hash, role, content, source_kind, source_agent, source_path, source_ref,
				scope_kind, scope_id, project_id, session_id, room, metadata_json,
				validity, claim_key, supersedes, superseded_by, created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, hash, p.Role, p.Content, p.SourceKind, p.SourceAgent, p.SourcePath, p.SourceRef,
		p.ScopeKind, p.ScopeID, p.ProjectID, p.SessionID, p.Room, p.MetadataJSON,
		lifecycle.Validity, lifecycle.ClaimKey, lifecycle.Supersedes, lifecycle.SupersededBy, createdAt,
	)
	if err != nil {
		return nil, false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return nil, false, err
	}
	mem, err := s.memoryByHash(ctx, hash)
	if err != nil {
		return nil, false, err
	}
	if affected == 0 && mem.TombstonedAt != "" {
		if _, err := s.db.ExecContext(ctx, `
			UPDATE memories
			SET tombstoned_at = NULL, purge_reason = NULL
			WHERE hash = ?`, hash); err != nil {
			return nil, false, err
		}
		mem, err = s.memoryByHash(ctx, hash)
		if err != nil {
			return nil, false, err
		}
		return mem, false, nil
	}
	return mem, affected == 0, nil
}

func (s *Store) Search(ctx context.Context, p SearchParams) ([]Memory, error) {
	ftsText := p.Query
	if p.SignalRerank {
		ftsText = retrieval.ExpandedQueryText(p.Query)
	}
	query := FTSQuery(ftsText)
	if query == "" {
		return nil, nil
	}
	if p.Limit <= 0 {
		p.Limit = 5
	}
	if p.Limit > 100 {
		p.Limit = 100
	}
	limit := p.Limit
	sqlLimit := limit
	temporalCue := p.SignalRerank && retrieval.HasTemporalCue(p.Query)
	if p.SignalRerank {
		// Always widen the candidate pool when reranking; otherwise FTS's `LIMIT
		// <limit>` clamp can drop high-prior docs (e.g. root README earning a
		// +3 wantsOverview boost) before the prior even runs.
		sqlLimit = maxInt(sqlLimit, maxInt(limit*10, 50))
	}
	if temporalCue {
		// Temporal cues benefit from a wider date-aware re-rank pool. Use the
		// max of the existing SignalRerank widening and the temporal-specific
		// width so we never *shrink* the pool when both apply.
		sqlLimit = maxInt(sqlLimit, maxInt(limit*4, 20))
	}
	if sqlLimit > 100 {
		sqlLimit = 100
	}
	where, args, err := scopedFilter("m", memoryQueryFilter{
		ScopeKind:   p.ScopeKind,
		ScopeID:     p.ScopeID,
		SourceKind:  p.SourceKind,
		SourceAgent: p.SourceAgent,
		SourcePath:  p.SourcePath,
		Role:        p.Role,
		ClaimKey:    p.ClaimKey,
		Validity:    p.Validity,
		Since:       p.Since,
		Before:      p.Before,
		Lifecycle:   p.Lifecycle,
	})
	if err != nil {
		return nil, err
	}
	args = append([]any{query}, args...)
	queryLower := strings.ToLower(strings.TrimSpace(p.Query))
	args = append(args,
		queryLower,
		queryLower,
		queryLower,
		queryLower,
		"%"+queryLower+"%",
		"%"+queryLower+"%",
		sqlLimit,
	)
	sqlText := `
			SELECT
				m.id, m.hash, COALESCE(m.role, ''), m.content,
				COALESCE(m.source_kind, 'direct'), COALESCE(m.source_agent, ''), COALESCE(m.source_path, ''), COALESCE(m.source_ref, ''),
				m.scope_kind, m.scope_id, COALESCE(m.project_id, ''), COALESCE(m.session_id, ''),
				COALESCE(m.room, ''), COALESCE(m.metadata_json, ''),
				COALESCE(m.validity, 'unknown'), COALESCE(m.claim_key, ''), COALESCE(m.supersedes, ''), COALESCE(m.superseded_by, ''),
				m.created_at, COALESCE(m.tombstoned_at, ''),
				bm25(memories_fts) AS rank,
				snippet(memories_fts, 0, '', '', '...', 18) AS excerpt
		FROM memories_fts
		JOIN memories m ON m.pk = memories_fts.rowid
		WHERE memories_fts MATCH ?` + where + `
		ORDER BY rank
			- CASE WHEN lower(COALESCE(m.claim_key, '')) = ? THEN 5.0 ELSE 0 END
			- CASE WHEN lower(COALESCE(m.role, '')) = ? THEN 3.0 ELSE 0 END
			- CASE WHEN lower(COALESCE(m.source_agent, '')) = ? THEN 1.5 ELSE 0 END
			- CASE WHEN lower(COALESCE(m.source_path, '')) = ? THEN 1.5 ELSE 0 END
			- CASE WHEN lower(COALESCE(m.source_path, '')) LIKE ? THEN 1.0 ELSE 0 END
			- CASE WHEN lower(m.content) LIKE ? THEN 1.0 ELSE 0 END
			- CASE WHEN lower(COALESCE(m.role, '')) IN ('adr', 'decision', 'constraint', 'preference', 'rule') THEN 3.0 ELSE 0 END
			- CASE WHEN COALESCE(m.source_kind, 'direct') = 'session_evidence' THEN 0.75 ELSE 0 END
			- CASE WHEN COALESCE(m.source_kind, 'direct') = 'direct' THEN 0.5 ELSE 0 END
			+ CASE WHEN COALESCE(m.source_kind, 'direct') = 'file' THEN 0.25 ELSE 0 END
		LIMIT ?`
	rows, err := s.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []Memory
	for rows.Next() {
		var mem Memory
		var rank float64
		if err := rows.Scan(
			&mem.ID, &mem.Hash, &mem.Role, &mem.Content, &mem.SourceKind, &mem.SourceAgent, &mem.SourcePath, &mem.SourceRef,
			&mem.ScopeKind, &mem.ScopeID, &mem.ProjectID, &mem.SessionID, &mem.Room, &mem.MetadataJSON,
			&mem.Validity, &mem.ClaimKey, &mem.Supersedes, &mem.SupersededBy,
			&mem.CreatedAt, &mem.TombstonedAt, &rank, &mem.Excerpt,
		); err != nil {
			return nil, err
		}
		mem.Score = retrievalScore(rank)
		if p.SignalRerank {
			mem.Score += sourcequality.ScorePriorWithOptions(p.Query, mem.SourcePath, mem.MetadataJSON, sourcequality.ModeSearch, p.SourceQuality)
		}
		results = append(results, mem)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if temporalCue && len(results) > 1 {
		queryDate := parseFlexibleTime(p.QueryDate)
		if queryDate.IsZero() {
			queryDate = time.Now().UTC()
		}
		sort.SliceStable(results, func(i, j int) bool {
			left := temporalSearchScore(p.Query, queryDate, results[i])
			right := temporalSearchScore(p.Query, queryDate, results[j])
			if math.Abs(left-right) < 1e-9 {
				return results[i].CreatedAt > results[j].CreatedAt
			}
			return left > right
		})
		if len(results) > limit {
			results = results[:limit]
		}
	}
	if p.SignalRerank && !temporalCue && len(results) > 1 {
		sort.SliceStable(results, func(i, j int) bool {
			if math.Abs(results[i].Score-results[j].Score) < 1e-9 {
				return results[i].CreatedAt > results[j].CreatedAt
			}
			return results[i].Score > results[j].Score
		})
	}
	return results, nil
}

func retrievalScore(rank float64) float64 {
	absRank := math.Abs(rank)
	return absRank / (1 + absRank)
}

func temporalSearchScore(query string, queryDate time.Time, mem Memory) float64 {
	sourceDate := parseMemoryTime(mem)
	evidence := temporalLexicalEvidence(query, mem.Content+" "+mem.SourceRef+" "+mem.SourcePath)
	return mem.Score + retrieval.TemporalScore(queryDate, sourceDate, query, mem.Content, "", evidence)
}

func temporalLexicalEvidence(query, source string) float64 {
	tokens := retrieval.SignificantTokens(query)
	if len(tokens) == 0 || strings.TrimSpace(source) == "" {
		return 0
	}
	sourceLower := strings.ToLower(source)
	hits := 0
	for _, tok := range tokens {
		if strings.Contains(sourceLower, tok) {
			hits++
		}
	}
	return 0.02 * float64(hits) / math.Sqrt(float64(len(tokens))+1)
}

func parseMemoryTime(mem Memory) time.Time {
	if t := parseFlexibleTime(mem.SourceRef); !t.IsZero() {
		return t
	}
	return parseFlexibleTime(mem.CreatedAt)
}

func parseFlexibleTime(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	if idx := strings.Index(value, " ("); idx > 0 {
		if end := strings.LastIndex(value, ")"); end > idx {
			value = strings.TrimSpace(value[:idx] + value[end+1:])
		}
	}
	for _, layout := range []string{time.RFC3339, "2006/01/02 15:04", "2006/01/02", "2006-01-02"} {
		if t, err := time.Parse(layout, value); err == nil {
			return t
		}
	}
	return time.Time{}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func EmbeddingText(mem Memory) string {
	parts := []string{
		mem.Role,
		strings.ReplaceAll(mem.ClaimKey, ".", " "),
		mem.ClaimKey,
		mem.SourceKind,
		mem.SourceAgent,
		strings.ReplaceAll(mem.SourcePath, "/", " "),
		mem.SourceRef,
		mem.Content,
	}
	var b strings.Builder
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(part)
	}
	return b.String()
}

func EmbeddingContentHash(mem Memory) string {
	sum := sha256.Sum256([]byte(EmbeddingText(mem)))
	return hex.EncodeToString(sum[:])
}

func cosineSimilarity(a, b []float64) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	score := dot / (math.Sqrt(normA) * math.Sqrt(normB))
	if score < 0 {
		return 0
	}
	if score > 1 {
		return 1
	}
	return score
}

func (s *Store) List(ctx context.Context, p ListParams) ([]Memory, error) {
	if p.Limit <= 0 {
		p.Limit = 50
	}
	if p.Limit > 1000 {
		p.Limit = 1000
	}
	where, args, err := scopedFilter("", memoryQueryFilter{
		ScopeKind:      p.ScopeKind,
		ScopeID:        p.ScopeID,
		SourceKind:     p.SourceKind,
		SourceAgent:    p.SourceAgent,
		SourcePath:     p.SourcePath,
		Role:           p.Role,
		ClaimKey:       p.ClaimKey,
		Validity:       p.Validity,
		Since:          p.Since,
		Before:         p.Before,
		IncludeDeleted: p.IncludeDeleted,
		Lifecycle:      p.Lifecycle,
	})
	if err != nil {
		return nil, err
	}
	args = append(args, p.Limit)
	rows, err := s.db.QueryContext(ctx, `
			SELECT id, hash, COALESCE(role, ''), content,
				COALESCE(source_kind, 'direct'), COALESCE(source_agent, ''), COALESCE(source_path, ''), COALESCE(source_ref, ''),
				scope_kind, scope_id, COALESCE(project_id, ''), COALESCE(session_id, ''),
				COALESCE(room, ''), COALESCE(metadata_json, ''),
				COALESCE(validity, 'unknown'), COALESCE(claim_key, ''), COALESCE(supersedes, ''), COALESCE(superseded_by, ''),
				created_at, COALESCE(tombstoned_at, '')
			FROM memories
		WHERE 1=1`+where+`
		ORDER BY created_at DESC, id DESC
		LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var memories []Memory
	for rows.Next() {
		mem, err := scanMemory(rows)
		if err != nil {
			return nil, err
		}
		memories = append(memories, mem)
	}
	return memories, rows.Err()
}

func (s *Store) UpsertEmbedding(ctx context.Context, mem Memory, provider, model string, vector []float64) error {
	provider = strings.TrimSpace(provider)
	model = strings.TrimSpace(model)
	if provider == "" || model == "" {
		return fmt.Errorf("embedding provider and model are required")
	}
	if len(vector) == 0 {
		return fmt.Errorf("embedding vector is empty")
	}
	vectorJSON, err := json.Marshal(vector)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO memory_embeddings (
			memory_id, provider, model, dims, vector_json, content_hash, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(memory_id, provider, model) DO UPDATE SET
			dims = excluded.dims,
			vector_json = excluded.vector_json,
			content_hash = excluded.content_hash,
			created_at = excluded.created_at`,
		mem.ID, provider, model, len(vector), string(vectorJSON), EmbeddingContentHash(mem), now,
	)
	return err
}

func (s *Store) SemanticSearch(ctx context.Context, p SemanticSearchParams) ([]Memory, error) {
	if len(p.QueryVector) == 0 {
		return nil, nil
	}
	if p.Limit <= 0 {
		p.Limit = 5
	}
	if p.Limit > 100 {
		p.Limit = 100
	}
	provider := strings.TrimSpace(p.Provider)
	model := strings.TrimSpace(p.Model)
	if provider == "" || model == "" {
		return nil, fmt.Errorf("embedding provider and model are required")
	}
	where, args, err := scopedFilter("m", memoryQueryFilter{
		ScopeKind:   p.ScopeKind,
		ScopeID:     p.ScopeID,
		SourceKind:  p.SourceKind,
		SourceAgent: p.SourceAgent,
		SourcePath:  p.SourcePath,
		Role:        p.Role,
		ClaimKey:    p.ClaimKey,
		Validity:    p.Validity,
		Since:       p.Since,
		Before:      p.Before,
		Lifecycle:   p.Lifecycle,
	})
	if err != nil {
		return nil, err
	}
	args = append([]any{provider, model}, args...)
	rows, err := s.db.QueryContext(ctx, `
			SELECT
				m.id, m.hash, COALESCE(m.role, ''), m.content,
				COALESCE(m.source_kind, 'direct'), COALESCE(m.source_agent, ''), COALESCE(m.source_path, ''), COALESCE(m.source_ref, ''),
				m.scope_kind, m.scope_id, COALESCE(m.project_id, ''), COALESCE(m.session_id, ''),
				COALESCE(m.room, ''), COALESCE(m.metadata_json, ''),
				COALESCE(m.validity, 'unknown'), COALESCE(m.claim_key, ''), COALESCE(m.supersedes, ''), COALESCE(m.superseded_by, ''),
				m.created_at, COALESCE(m.tombstoned_at, ''),
				e.vector_json, e.content_hash
			FROM memory_embeddings e
			JOIN memories m ON m.id = e.memory_id
			WHERE e.provider = ? AND e.model = ?`+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ranked []Memory
	for rows.Next() {
		var mem Memory
		var vectorJSON, contentHash string
		if err := rows.Scan(
			&mem.ID, &mem.Hash, &mem.Role, &mem.Content, &mem.SourceKind, &mem.SourceAgent, &mem.SourcePath, &mem.SourceRef,
			&mem.ScopeKind, &mem.ScopeID, &mem.ProjectID, &mem.SessionID, &mem.Room, &mem.MetadataJSON,
			&mem.Validity, &mem.ClaimKey, &mem.Supersedes, &mem.SupersededBy,
			&mem.CreatedAt, &mem.TombstonedAt, &vectorJSON, &contentHash,
		); err != nil {
			return nil, err
		}
		if contentHash != EmbeddingContentHash(mem) {
			continue
		}
		var vector []float64
		if err := json.Unmarshal([]byte(vectorJSON), &vector); err != nil {
			return nil, err
		}
		score := cosineSimilarity(p.QueryVector, vector)
		if score <= 0 {
			continue
		}
		mem.Score = score
		mem.Excerpt = mem.Content
		ranked = append(ranked, mem)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].Score == ranked[j].Score {
			return ranked[i].CreatedAt > ranked[j].CreatedAt
		}
		return ranked[i].Score > ranked[j].Score
	})
	if len(ranked) > p.Limit {
		ranked = ranked[:p.Limit]
	}
	return ranked, nil
}

func (s *Store) UpdateLifecycle(ctx context.Context, p LifecycleParams) (*Memory, error) {
	id, err := s.resolveMemoryID(ctx, p.IDOrPrefix, false)
	if err != nil {
		return nil, err
	}
	validity, err := normalizeValidity(p.Validity)
	if err != nil {
		return nil, err
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE memories
		SET validity = ?, claim_key = ?, supersedes = ?, superseded_by = ?
		WHERE id = ?`,
		validity,
		emptyToNull(strings.TrimSpace(p.ClaimKey)),
		emptyToNull(strings.TrimSpace(p.Supersedes)),
		emptyToNull(strings.TrimSpace(p.SupersededBy)),
		id,
	); err != nil {
		return nil, err
	}
	return s.GetMemoryByID(ctx, id, false)
}

func (s *Store) UpdateMetadata(ctx context.Context, p MetadataParams) (*Memory, error) {
	id, err := s.resolveMemoryID(ctx, p.IDOrPrefix, false)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.MetadataJSON) != "" {
		var metadata map[string]any
		if err := json.Unmarshal([]byte(p.MetadataJSON), &metadata); err != nil {
			return nil, fmt.Errorf("metadata_json must be a JSON object: %w", err)
		}
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE memories
		SET metadata_json = ?
		WHERE id = ?`,
		emptyToNull(strings.TrimSpace(p.MetadataJSON)),
		id,
	); err != nil {
		return nil, err
	}
	return s.GetMemoryByID(ctx, id, false)
}

func (s *Store) ForgetMemory(ctx context.Context, p ForgetParams) (ForgetResult, error) {
	id, err := s.resolveMemoryID(ctx, p.IDOrPrefix, true)
	if err != nil {
		return ForgetResult{}, err
	}
	mem, err := s.GetMemoryByID(ctx, id, true)
	if err != nil {
		return ForgetResult{}, err
	}
	if p.Destroy {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM memories WHERE id = ?`, id); err != nil {
			return ForgetResult{}, err
		}
		return ForgetResult{Memory: *mem, Destroyed: true}, nil
	}
	if mem.TombstonedAt == "" {
		now := time.Now().UTC().Format(time.RFC3339)
		if _, err := s.db.ExecContext(ctx, `UPDATE memories SET tombstoned_at = ?, purge_reason = ? WHERE id = ?`, now, p.Reason, id); err != nil {
			return ForgetResult{}, err
		}
		mem.TombstonedAt = now
	}
	return ForgetResult{Memory: *mem}, nil
}

func (s *Store) ForgetByFilter(ctx context.Context, p ListParams, reason string, destroy bool) (BulkForgetResult, error) {
	p.IncludeDeleted = destroy
	p.Limit = 1000
	memories, err := s.List(ctx, p)
	if err != nil {
		return BulkForgetResult{}, err
	}
	ids := make([]string, 0, len(memories))
	for _, mem := range memories {
		ids = append(ids, mem.ID)
	}
	if len(ids) == 0 {
		return BulkForgetResult{IDs: ids, Destroyed: destroy}, nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return BulkForgetResult{}, err
	}
	defer tx.Rollback()

	if destroy {
		stmt, err := tx.PrepareContext(ctx, `DELETE FROM memories WHERE id = ?`)
		if err != nil {
			return BulkForgetResult{}, err
		}
		defer stmt.Close()
		for _, id := range ids {
			if _, err := stmt.ExecContext(ctx, id); err != nil {
				return BulkForgetResult{}, err
			}
		}
	} else {
		now := time.Now().UTC().Format(time.RFC3339)
		stmt, err := tx.PrepareContext(ctx, `UPDATE memories SET tombstoned_at = ?, purge_reason = ? WHERE id = ? AND tombstoned_at IS NULL`)
		if err != nil {
			return BulkForgetResult{}, err
		}
		defer stmt.Close()
		for _, id := range ids {
			if _, err := stmt.ExecContext(ctx, now, reason, id); err != nil {
				return BulkForgetResult{}, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return BulkForgetResult{}, err
	}
	return BulkForgetResult{IDs: ids, Count: len(ids), Destroyed: destroy}, nil
}

func (s *Store) DestroySourceMemories(ctx context.Context, scopeKind, scopeID, sourceKind, sourcePath string) (BulkForgetResult, error) {
	scopeKind = strings.TrimSpace(scopeKind)
	scopeID = strings.TrimSpace(scopeID)
	sourceKind = normalizeSourceKind(sourceKind)
	sourcePath = strings.TrimSpace(filepath.ToSlash(sourcePath))
	if scopeKind == "" || scopeID == "" || sourcePath == "" {
		return BulkForgetResult{}, fmt.Errorf("scope and source path are required")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id
		FROM memories
		WHERE scope_kind = ?
			AND scope_id = ?
			AND COALESCE(source_kind, 'direct') = ?
			AND COALESCE(source_path, '') = ?`,
		scopeKind, scopeID, sourceKind, sourcePath)
	if err != nil {
		return BulkForgetResult{}, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return BulkForgetResult{}, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return BulkForgetResult{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return BulkForgetResult{}, err
	}
	defer tx.Rollback()
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `DELETE FROM memories WHERE id = ?`, id); err != nil {
			return BulkForgetResult{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM sources
		WHERE scope_kind = ?
			AND scope_id = ?
			AND kind = ?
			AND COALESCE(path, '') = ?`,
		scopeKind, scopeID, sourceKind, sourcePath); err != nil {
		return BulkForgetResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return BulkForgetResult{}, err
	}
	return BulkForgetResult{IDs: ids, Count: len(ids), Destroyed: true}, nil
}

func (s *Store) GetMemoryByID(ctx context.Context, id string, includeDeleted bool) (*Memory, error) {
	deletedClause := "AND tombstoned_at IS NULL"
	if includeDeleted {
		deletedClause = ""
	}
	row := s.db.QueryRowContext(ctx, `
			SELECT id, hash, COALESCE(role, ''), content,
				COALESCE(source_kind, 'direct'), COALESCE(source_agent, ''), COALESCE(source_path, ''), COALESCE(source_ref, ''),
				scope_kind, scope_id, COALESCE(project_id, ''), COALESCE(session_id, ''),
				COALESCE(room, ''), COALESCE(metadata_json, ''),
				COALESCE(validity, 'unknown'), COALESCE(claim_key, ''), COALESCE(supersedes, ''), COALESCE(superseded_by, ''),
				created_at, COALESCE(tombstoned_at, '')
			FROM memories
		WHERE id = ? `+deletedClause, id)
	mem, err := scanMemory(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &mem, nil
}

func (s *Store) Repair(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO memories_fts(memories_fts) VALUES('rebuild')`)
	return formatSQLiteFeatureError(err)
}

func (s *Store) GetMemory(ctx context.Context, idOrPrefix string) (*Memory, error) {
	id, err := s.resolveMemoryID(ctx, idOrPrefix, false)
	if err != nil {
		return nil, err
	}
	return s.GetMemoryByID(ctx, id, false)
}

func (s *Store) Counts(ctx context.Context) (Counts, error) {
	var counts Counts
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memories`).Scan(&counts.Total); err != nil {
		return counts, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memories WHERE tombstoned_at IS NULL`).Scan(&counts.Active); err != nil {
		return counts, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memories WHERE tombstoned_at IS NOT NULL`).Scan(&counts.Tombstoned); err != nil {
		return counts, err
	}
	return counts, nil
}

func (s *Store) FileSources(ctx context.Context, scopeKind, scopeID, agent string) ([]FileSource, error) {
	args := []any{strings.TrimSpace(scopeKind), strings.TrimSpace(scopeID)}
	agentWhere := ""
	if strings.TrimSpace(agent) != "" {
		agentWhere = "AND COALESCE(agent, '') = ?"
		args = append(args, strings.TrimSpace(agent))
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, COALESCE(path, ''), COALESCE(agent, ''), COALESCE(scope_kind, ''),
			COALESCE(scope_id, ''), COALESCE(content_hash, ''), COALESCE(mod_time, ''),
			COALESCE(size_bytes, 0), COALESCE(chunk_count, 0), COALESCE(last_mined_at, ''),
			COALESCE(deleted_at, '')
		FROM sources
		WHERE kind = 'file'
			AND scope_kind = ?
			AND scope_id = ?
			AND deleted_at IS NULL
			`+agentWhere+`
		ORDER BY path, agent`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sources []FileSource
	for rows.Next() {
		var src FileSource
		if err := rows.Scan(
			&src.ID, &src.Path, &src.Agent, &src.ScopeKind, &src.ScopeID,
			&src.ContentHash, &src.ModTime, &src.SizeBytes, &src.ChunkCount,
			&src.LastMinedAt, &src.DeletedAt,
		); err != nil {
			return nil, err
		}
		sources = append(sources, src)
	}
	return sources, rows.Err()
}

func (s *Store) RefreshSource(ctx context.Context, p SourceRefreshParams) (SourceRefreshResult, error) {
	if strings.TrimSpace(p.Kind) == "" {
		p.Kind = "file"
	}
	p.Kind = normalizeSourceKind(p.Kind)
	p.Path = strings.TrimSpace(filepath.ToSlash(p.Path))
	p.Agent = strings.TrimSpace(p.Agent)
	p.ScopeKind = strings.TrimSpace(p.ScopeKind)
	p.ScopeID = strings.TrimSpace(p.ScopeID)
	if p.Path == "" || p.ScopeKind == "" || p.ScopeID == "" {
		return SourceRefreshResult{}, fmt.Errorf("source path and scope are required")
	}
	id := sourceID(p.Kind, p.ScopeKind, p.ScopeID, p.Agent, p.Path)
	var previousHash string
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(content_hash, '') FROM sources WHERE id = ?`, id).Scan(&previousHash)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return SourceRefreshResult{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	changed := errors.Is(err, sql.ErrNoRows) || previousHash != strings.TrimSpace(p.ContentHash)
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO sources (
			id, kind, path, agent, scope_kind, scope_id, content_hash,
			mod_time, size_bytes, chunk_count, last_mined_at, metadata_json, deleted_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL)
		ON CONFLICT(id) DO UPDATE SET
			kind = excluded.kind,
			path = excluded.path,
			agent = excluded.agent,
			scope_kind = excluded.scope_kind,
			scope_id = excluded.scope_id,
			content_hash = excluded.content_hash,
			mod_time = excluded.mod_time,
			size_bytes = excluded.size_bytes,
			chunk_count = excluded.chunk_count,
			last_mined_at = excluded.last_mined_at,
			metadata_json = excluded.metadata_json,
			deleted_at = NULL`,
		id, p.Kind, p.Path, p.Agent, p.ScopeKind, p.ScopeID, strings.TrimSpace(p.ContentHash),
		strings.TrimSpace(p.ModTime), p.Size, p.ChunkCount, now, p.MetadataJSON,
	); err != nil {
		return SourceRefreshResult{}, err
	}
	for _, id := range p.ActiveMemoryIDs {
		if _, err := s.db.ExecContext(ctx, `
			UPDATE memories
			SET validity = 'active'
			WHERE id = ?
				AND scope_kind = ?
				AND scope_id = ?
				AND COALESCE(source_agent, '') = ?
				AND COALESCE(source_path, '') = ?
				AND COALESCE(validity, 'unknown') = 'stale'`,
			id, p.ScopeKind, p.ScopeID, p.Agent, p.Path,
		); err != nil {
			return SourceRefreshResult{}, err
		}
	}
	staled, err := s.staleSourceMemoriesExcept(ctx, p, p.ActiveMemoryIDs)
	if err != nil {
		return SourceRefreshResult{}, err
	}
	return SourceRefreshResult{ID: id, Changed: changed, Staled: staled}, nil
}

func (s *Store) StaleMissingSources(ctx context.Context, scopeKind, scopeID, agent string, currentPaths []string) (int, error) {
	current := make(map[string]bool, len(currentPaths))
	for _, path := range currentPaths {
		current[filepath.ToSlash(strings.TrimSpace(path))] = true
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, path, COALESCE(agent, '')
		FROM sources
		WHERE kind = 'file'
			AND scope_kind = ?
			AND scope_id = ?
			AND COALESCE(agent, '') = ?
			AND deleted_at IS NULL`,
		scopeKind, scopeID, strings.TrimSpace(agent),
	)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	type sourceRow struct {
		id    string
		path  string
		agent string
	}
	var missing []sourceRow
	for rows.Next() {
		var row sourceRow
		if err := rows.Scan(&row.id, &row.path, &row.agent); err != nil {
			return 0, err
		}
		if !current[filepath.ToSlash(row.path)] {
			missing = append(missing, row)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	total := 0
	now := time.Now().UTC().Format(time.RFC3339)
	for _, row := range missing {
		if _, err := s.db.ExecContext(ctx, `UPDATE sources SET deleted_at = ? WHERE id = ?`, now, row.id); err != nil {
			return total, err
		}
		staled, err := s.staleSourceMemoriesExcept(ctx, SourceRefreshParams{
			Kind:      "file",
			Path:      row.path,
			Agent:     row.agent,
			ScopeKind: scopeKind,
			ScopeID:   scopeID,
		}, nil)
		if err != nil {
			return total, err
		}
		total += staled
	}
	orphanStaled, err := s.staleMissingFileMemories(ctx, scopeKind, scopeID, strings.TrimSpace(agent), current)
	if err != nil {
		return total, err
	}
	total += orphanStaled
	return total, nil
}

func (s *Store) staleSourceMemoriesExcept(ctx context.Context, p SourceRefreshParams, keepIDs []string) (int, error) {
	p.Kind = normalizeSourceKind(p.Kind)
	keep := make(map[string]bool, len(keepIDs))
	for _, id := range keepIDs {
		keep[id] = true
	}
	roleWhere := ""
	kindWhere := ""
	args := []any{p.ScopeKind, p.ScopeID, p.Agent, p.Path}
	if p.Kind != "" && p.Kind != "direct" {
		kindWhere = "AND COALESCE(source_kind, 'direct') = ?"
		args = append(args, p.Kind)
	}
	if strings.TrimSpace(p.Role) != "" {
		roleWhere = "AND COALESCE(role, '') = ?"
		args = append(args, strings.TrimSpace(p.Role))
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id
		FROM memories
		WHERE scope_kind = ?
			AND scope_id = ?
			AND COALESCE(source_agent, '') = ?
			AND COALESCE(source_path, '') = ?
			`+kindWhere+`
			AND tombstoned_at IS NULL
			AND COALESCE(validity, 'unknown') NOT IN ('historical', 'rejected', 'superseded', 'stale', 'tombstoned')
			`+roleWhere, args...)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	staled := 0
	for _, id := range ids {
		if keep[id] {
			continue
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE memories SET validity = 'stale' WHERE id = ?`, id); err != nil {
			return staled, err
		}
		staled++
	}
	return staled, nil
}

func (s *Store) staleMissingFileMemories(ctx context.Context, scopeKind, scopeID, agent string, current map[string]bool) (int, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT COALESCE(source_path, '')
		FROM memories
		WHERE scope_kind = ?
			AND scope_id = ?
			AND COALESCE(source_agent, '') = ?
			AND COALESCE(source_kind, 'direct') = 'file'
			AND COALESCE(source_path, '') != ''
			AND tombstoned_at IS NULL
			AND COALESCE(validity, 'unknown') NOT IN ('historical', 'rejected', 'superseded', 'stale', 'tombstoned')`,
		scopeKind, scopeID, agent)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	total := 0
	for rows.Next() {
		var sourcePath string
		if err := rows.Scan(&sourcePath); err != nil {
			return total, err
		}
		if current[filepath.ToSlash(strings.TrimSpace(sourcePath))] {
			continue
		}
		staled, err := s.staleSourceMemoriesExcept(ctx, SourceRefreshParams{
			Kind:      "file",
			Path:      sourcePath,
			Agent:     agent,
			ScopeKind: scopeKind,
			ScopeID:   scopeID,
		}, nil)
		if err != nil {
			return total, err
		}
		total += staled
	}
	return total, rows.Err()
}

func sourceID(kind, scopeKind, scopeID, agent, sourcePath string) string {
	h := sha256.New()
	for _, part := range []string{normalizeSourceKind(kind), scopeKind, scopeID, agent, filepath.ToSlash(sourcePath)} {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	return "src_" + hex.EncodeToString(h.Sum(nil))[:24]
}

func (s *Store) memoryByHash(ctx context.Context, hash string) (*Memory, error) {
	row := s.db.QueryRowContext(ctx, `
			SELECT id, hash, COALESCE(role, ''), content,
				COALESCE(source_kind, 'direct'), COALESCE(source_agent, ''), COALESCE(source_path, ''), COALESCE(source_ref, ''),
				scope_kind, scope_id, COALESCE(project_id, ''), COALESCE(session_id, ''),
				COALESCE(room, ''), COALESCE(metadata_json, ''),
				COALESCE(validity, 'unknown'), COALESCE(claim_key, ''), COALESCE(supersedes, ''), COALESCE(superseded_by, ''),
				created_at, COALESCE(tombstoned_at, '')
			FROM memories
		WHERE hash = ?`, hash)
	mem, err := scanMemory(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &mem, nil
}

func (s *Store) resolveMemoryID(ctx context.Context, idOrPrefix string, includeDeleted bool) (string, error) {
	idOrPrefix = strings.TrimSpace(idOrPrefix)
	if idOrPrefix == "" {
		return "", ErrNotFound
	}
	deletedClause := "AND tombstoned_at IS NULL"
	if includeDeleted {
		deletedClause = ""
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id
		FROM memories
		WHERE (id = ? OR id LIKE ?) `+deletedClause+`
		ORDER BY id
		LIMIT 2`, idOrPrefix, idOrPrefix+"%")
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return "", err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if len(ids) == 0 {
		return "", ErrNotFound
	}
	if len(ids) > 1 {
		return "", ErrAmbiguous
	}
	return ids[0], nil
}

type memoryScanner interface {
	Scan(dest ...any) error
}

func scanMemory(scanner memoryScanner) (Memory, error) {
	var mem Memory
	err := scanner.Scan(
		&mem.ID, &mem.Hash, &mem.Role, &mem.Content, &mem.SourceKind, &mem.SourceAgent, &mem.SourcePath, &mem.SourceRef,
		&mem.ScopeKind, &mem.ScopeID, &mem.ProjectID, &mem.SessionID, &mem.Room, &mem.MetadataJSON,
		&mem.Validity, &mem.ClaimKey, &mem.Supersedes, &mem.SupersededBy,
		&mem.CreatedAt, &mem.TombstonedAt,
	)
	return mem, err
}

type memoryQueryFilter struct {
	ScopeKind      string
	ScopeID        string
	SourceKind     string
	SourceAgent    string
	SourcePath     string
	Role           string
	ClaimKey       string
	Validity       string
	Since          string
	Before         string
	IncludeDeleted bool
	Lifecycle      string
}

func scopedFilter(alias string, filter memoryQueryFilter) (string, []any, error) {
	col := func(name string) string {
		if alias == "" {
			return name
		}
		return alias + "." + name
	}
	var where strings.Builder
	var args []any
	if !filter.IncludeDeleted {
		fmt.Fprintf(&where, "\n\t\t\tAND %s IS NULL", col("tombstoned_at"))
	}
	if filter.ScopeKind != "" {
		fmt.Fprintf(&where, "\n\t\t\tAND %s = ?", col("scope_kind"))
		args = append(args, filter.ScopeKind)
	}
	if filter.ScopeID != "" {
		fmt.Fprintf(&where, "\n\t\t\tAND %s = ?", col("scope_id"))
		args = append(args, filter.ScopeID)
	}
	if filter.SourceKind != "" {
		fmt.Fprintf(&where, "\n\t\t\tAND COALESCE(%s, 'direct') = ?", col("source_kind"))
		args = append(args, normalizeSourceKind(filter.SourceKind))
	}
	if filter.SourceAgent != "" {
		fmt.Fprintf(&where, "\n\t\t\tAND %s = ?", col("source_agent"))
		args = append(args, filter.SourceAgent)
	}
	if filter.SourcePath != "" {
		fmt.Fprintf(&where, "\n\t\t\tAND %s LIKE ?", col("source_path"))
		args = append(args, "%"+filter.SourcePath+"%")
	}
	if filter.Role != "" {
		fmt.Fprintf(&where, "\n\t\t\tAND %s = ?", col("role"))
		args = append(args, filter.Role)
	}
	if filter.ClaimKey != "" {
		fmt.Fprintf(&where, "\n\t\t\tAND %s = ?", col("claim_key"))
		args = append(args, filter.ClaimKey)
	}
	if filter.Validity != "" {
		fmt.Fprintf(&where, "\n\t\t\tAND COALESCE(%s, 'unknown') = ?", col("validity"))
		args = append(args, filter.Validity)
	}
	if filter.Since != "" {
		fmt.Fprintf(&where, "\n\t\t\tAND %s >= ?", col("created_at"))
		args = append(args, filter.Since)
	}
	if filter.Before != "" {
		fmt.Fprintf(&where, "\n\t\t\tAND %s <= ?", col("created_at"))
		args = append(args, filter.Before)
	}
	lifecycleWhere, err := lifecycleFilter(col("validity"), filter.Lifecycle)
	if err != nil {
		return "", nil, err
	}
	where.WriteString(lifecycleWhere)
	return where.String(), args, nil
}

func lifecycleFilter(validityColumn, lifecycle string) (string, error) {
	switch strings.TrimSpace(lifecycle) {
	case LifecycleAny:
		return "", nil
	case LifecycleCurrent:
		return fmt.Sprintf("\n\t\t\tAND COALESCE(%s, 'unknown') NOT IN ('historical', 'rejected', 'superseded', 'stale', 'tombstoned')", validityColumn), nil
	case LifecycleHistorical:
		return fmt.Sprintf("\n\t\t\tAND COALESCE(%s, 'unknown') IN ('historical', 'rejected', 'superseded', 'stale', 'tombstoned')", validityColumn), nil
	default:
		return "", fmt.Errorf("invalid lifecycle filter %q", lifecycle)
	}
}

func (s *Store) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS memories (
			pk INTEGER PRIMARY KEY AUTOINCREMENT,
			id TEXT NOT NULL UNIQUE,
			hash TEXT NOT NULL UNIQUE,
			role TEXT,
			content TEXT NOT NULL,
			source_kind TEXT NOT NULL DEFAULT 'direct',
			source_agent TEXT,
			source_path TEXT,
			source_ref TEXT,
			scope_kind TEXT NOT NULL,
			scope_id TEXT NOT NULL,
			project_id TEXT,
				session_id TEXT,
				room TEXT,
				metadata_json TEXT,
				validity TEXT NOT NULL DEFAULT 'unknown',
				claim_key TEXT,
				supersedes TEXT,
				superseded_by TEXT,
				created_at TEXT NOT NULL,
				tombstoned_at TEXT,
				purge_reason TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_memories_scope ON memories(scope_kind, scope_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_memories_created ON memories(created_at)`,
		`CREATE TABLE IF NOT EXISTS sources (
			id TEXT PRIMARY KEY,
			kind TEXT NOT NULL,
			path TEXT,
			agent TEXT,
			scope_kind TEXT,
			scope_id TEXT,
			content_hash TEXT,
			mod_time TEXT,
			size_bytes INTEGER,
			chunk_count INTEGER,
			last_mined_at TEXT,
			deleted_at TEXT,
			metadata_json TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS memory_embeddings (
			memory_id TEXT NOT NULL,
			provider TEXT NOT NULL,
			model TEXT NOT NULL,
			dims INTEGER NOT NULL,
			vector_json TEXT NOT NULL,
			content_hash TEXT NOT NULL,
			created_at TEXT NOT NULL,
			PRIMARY KEY(memory_id, provider, model),
			FOREIGN KEY(memory_id) REFERENCES memories(id) ON DELETE CASCADE
		)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return formatSQLiteFeatureError(err)
		}
	}
	if err := s.ensureLifecycleColumns(); err != nil {
		return err
	}
	if err := s.ensureSourceColumns(); err != nil {
		return err
	}
	if err := s.ensureChannelTables(); err != nil {
		return err
	}
	for _, stmt := range []string{
		`CREATE INDEX IF NOT EXISTS idx_memories_validity ON memories(validity)`,
		`CREATE INDEX IF NOT EXISTS idx_memories_claim_scope ON memories(scope_kind, scope_id, claim_key)`,
		`CREATE INDEX IF NOT EXISTS idx_memories_source_kind ON memories(scope_kind, scope_id, source_kind)`,
		`CREATE INDEX IF NOT EXISTS idx_sources_scope_path ON sources(scope_kind, scope_id, path)`,
		`CREATE INDEX IF NOT EXISTS idx_memory_embeddings_provider ON memory_embeddings(provider, model)`,
	} {
		if _, err := s.db.Exec(stmt); err != nil {
			return err
		}
	}
	if err := s.backfillLifecycleFromMetadata(); err != nil {
		return err
	}
	if err := s.backfillSourceKind(); err != nil {
		return err
	}
	if err := s.ensureFTSSchema(); err != nil {
		return err
	}
	if _, err := s.db.Exec(`INSERT INTO memories_fts(memories_fts) VALUES('rebuild')`); err != nil {
		return formatSQLiteFeatureError(err)
	}
	return nil
}

func (s *Store) ensureSourceColumns() error {
	columns, err := s.tableColumns("sources")
	if err != nil {
		return err
	}
	alter := []struct {
		name string
		sql  string
	}{
		{name: "scope_kind", sql: `ALTER TABLE sources ADD COLUMN scope_kind TEXT`},
		{name: "scope_id", sql: `ALTER TABLE sources ADD COLUMN scope_id TEXT`},
		{name: "content_hash", sql: `ALTER TABLE sources ADD COLUMN content_hash TEXT`},
		{name: "mod_time", sql: `ALTER TABLE sources ADD COLUMN mod_time TEXT`},
		{name: "size_bytes", sql: `ALTER TABLE sources ADD COLUMN size_bytes INTEGER`},
		{name: "chunk_count", sql: `ALTER TABLE sources ADD COLUMN chunk_count INTEGER`},
		{name: "deleted_at", sql: `ALTER TABLE sources ADD COLUMN deleted_at TEXT`},
	}
	for _, col := range alter {
		if columns[col.name] {
			continue
		}
		if _, err := s.db.Exec(col.sql); err != nil {
			if isDuplicateColumnError(err) {
				continue
			}
			return err
		}
	}
	return nil
}

func (s *Store) ensureFTSSchema() error {
	columns, err := s.tableColumns("memories_fts")
	if err != nil {
		return err
	}
	expected := []string{"content", "role", "claim_key", "source_agent", "source_path", "source_ref"}
	recreate := len(columns) == 0
	for _, name := range expected {
		if !columns[name] {
			recreate = true
			break
		}
	}
	if recreate {
		for _, stmt := range []string{
			`DROP TRIGGER IF EXISTS memories_ai`,
			`DROP TRIGGER IF EXISTS memories_ad`,
			`DROP TRIGGER IF EXISTS memories_au`,
			`DROP TABLE IF EXISTS memories_fts`,
			`CREATE VIRTUAL TABLE memories_fts USING fts5(
				content,
				role,
				claim_key,
				source_agent,
				source_path,
				source_ref,
				content='memories',
				content_rowid='pk'
			)`,
		} {
			if _, err := s.db.Exec(stmt); err != nil {
				return formatSQLiteFeatureError(err)
			}
		}
	}
	for _, stmt := range []string{
		`DROP TRIGGER IF EXISTS memories_ai`,
		`DROP TRIGGER IF EXISTS memories_ad`,
		`DROP TRIGGER IF EXISTS memories_au`,
		`CREATE TRIGGER memories_ai AFTER INSERT ON memories BEGIN
			INSERT INTO memories_fts(rowid, content, role, claim_key, source_agent, source_path, source_ref)
			VALUES (new.pk, new.content, new.role, new.claim_key, new.source_agent, new.source_path, new.source_ref);
		END`,
		`CREATE TRIGGER memories_ad AFTER DELETE ON memories BEGIN
			INSERT INTO memories_fts(memories_fts, rowid, content, role, claim_key, source_agent, source_path, source_ref)
			VALUES('delete', old.pk, old.content, old.role, old.claim_key, old.source_agent, old.source_path, old.source_ref);
		END`,
		`CREATE TRIGGER memories_au AFTER UPDATE OF content, role, claim_key, source_agent, source_path, source_ref ON memories BEGIN
			INSERT INTO memories_fts(memories_fts, rowid, content, role, claim_key, source_agent, source_path, source_ref)
			VALUES('delete', old.pk, old.content, old.role, old.claim_key, old.source_agent, old.source_path, old.source_ref);
			INSERT INTO memories_fts(rowid, content, role, claim_key, source_agent, source_path, source_ref)
			VALUES (new.pk, new.content, new.role, new.claim_key, new.source_agent, new.source_path, new.source_ref);
		END`,
	} {
		if _, err := s.db.Exec(stmt); err != nil {
			return formatSQLiteFeatureError(err)
		}
	}
	return nil
}

func (s *Store) ensureLifecycleColumns() error {
	columns, err := s.tableColumns("memories")
	if err != nil {
		return err
	}
	alter := []struct {
		name string
		sql  string
	}{
		{name: "source_kind", sql: `ALTER TABLE memories ADD COLUMN source_kind TEXT NOT NULL DEFAULT 'direct'`},
		{name: "validity", sql: `ALTER TABLE memories ADD COLUMN validity TEXT NOT NULL DEFAULT 'unknown'`},
		{name: "claim_key", sql: `ALTER TABLE memories ADD COLUMN claim_key TEXT`},
		{name: "supersedes", sql: `ALTER TABLE memories ADD COLUMN supersedes TEXT`},
		{name: "superseded_by", sql: `ALTER TABLE memories ADD COLUMN superseded_by TEXT`},
	}
	for _, col := range alter {
		if columns[col.name] {
			continue
		}
		if _, err := s.db.Exec(col.sql); err != nil {
			if isDuplicateColumnError(err) {
				continue
			}
			return err
		}
	}
	return nil
}

func isDuplicateColumnError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "duplicate column name")
}

func (s *Store) tableColumns(table string) (map[string]bool, error) {
	rows, err := s.db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	columns := make(map[string]bool)
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var dfltValue any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dfltValue, &pk); err != nil {
			return nil, err
		}
		columns[name] = true
	}
	return columns, rows.Err()
}

func (s *Store) backfillLifecycleFromMetadata() error {
	rows, err := s.db.Query(`
		SELECT id, metadata_json
		FROM memories
		WHERE metadata_json IS NOT NULL AND metadata_json != ''
			AND (validity = 'unknown' OR claim_key IS NULL OR supersedes IS NULL OR superseded_by IS NULL)`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type update struct {
		id        string
		lifecycle memoryLifecycle
	}
	var updates []update
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			return err
		}
		lifecycle, ok, err := lifecycleFromMetadata(raw)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		updates = append(updates, update{id: id, lifecycle: lifecycle})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, item := range updates {
		validity := item.lifecycle.Validity
		if validity == "" {
			validity = "unknown"
		}
		if _, err := s.db.Exec(`
			UPDATE memories
			SET
				validity = CASE WHEN validity = 'unknown' THEN ? ELSE validity END,
				claim_key = COALESCE(claim_key, ?),
				supersedes = COALESCE(supersedes, ?),
				superseded_by = COALESCE(superseded_by, ?)
			WHERE id = ?`,
			validity,
			emptyToNull(item.lifecycle.ClaimKey),
			emptyToNull(item.lifecycle.Supersedes),
			emptyToNull(item.lifecycle.SupersededBy),
			item.id,
		); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) backfillSourceKind() error {
	if _, err := s.db.Exec(`
		UPDATE memories
		SET source_kind = 'file'
		WHERE COALESCE(source_kind, '') IN ('', 'direct')
			AND metadata_json LIKE '%"kind":"file_chunk"%'
			AND COALESCE(source_path, '') != ''`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`
		UPDATE memories
		SET source_kind = 'session_evidence'
		WHERE COALESCE(source_kind, '') IN ('', 'direct')
			AND metadata_json LIKE '%"kind":"session_evidence"%'
			AND COALESCE(source_path, '') != ''`); err != nil {
		return err
	}
	return nil
}

type memoryLifecycle struct {
	Validity     string
	ClaimKey     string
	Supersedes   string
	SupersededBy string
}

func lifecycleFromAddParams(p AddMemoryParams) (memoryLifecycle, error) {
	lifecycle := memoryLifecycle{
		ClaimKey:     strings.TrimSpace(p.ClaimKey),
		Supersedes:   strings.TrimSpace(p.Supersedes),
		SupersededBy: strings.TrimSpace(p.SupersededBy),
	}
	if metadata, ok, err := lifecycleFromMetadata(p.MetadataJSON); err != nil {
		return memoryLifecycle{}, err
	} else if ok {
		if strings.TrimSpace(p.Validity) == "" {
			lifecycle.Validity = metadata.Validity
		}
		if lifecycle.ClaimKey == "" {
			lifecycle.ClaimKey = metadata.ClaimKey
		}
		if lifecycle.Supersedes == "" {
			lifecycle.Supersedes = metadata.Supersedes
		}
		if lifecycle.SupersededBy == "" {
			lifecycle.SupersededBy = metadata.SupersededBy
		}
	}
	if lifecycle.Validity == "" {
		lifecycle.Validity = p.Validity
	}
	validity, err := normalizeValidity(lifecycle.Validity)
	if err != nil {
		return memoryLifecycle{}, err
	}
	lifecycle.Validity = validity
	return lifecycle, nil
}

func lifecycleFromMetadata(raw string) (memoryLifecycle, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || !json.Valid([]byte(raw)) {
		return memoryLifecycle{}, false, nil
	}
	var metadata map[string]any
	if err := json.Unmarshal([]byte(raw), &metadata); err != nil {
		return memoryLifecycle{}, false, err
	}
	lifecycle := memoryLifecycle{
		ClaimKey:     metadataString(metadata, "claim_key"),
		Supersedes:   metadataString(metadata, "supersedes"),
		SupersededBy: metadataString(metadata, "superseded_by"),
	}
	validityRaw := metadataString(metadata, "validity")
	if validityRaw != "" {
		validity, err := normalizeValidity(validityRaw)
		if err == nil {
			lifecycle.Validity = validity
		}
	}
	hasLifecycle := lifecycle.Validity != "" ||
		lifecycle.ClaimKey != "" ||
		lifecycle.Supersedes != "" ||
		lifecycle.SupersededBy != ""
	return lifecycle, hasLifecycle, nil
}

func metadataString(metadata map[string]any, key string) string {
	value, ok := metadata[key]
	if !ok {
		return ""
	}
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				parts = append(parts, strings.TrimSpace(s))
			}
		}
		return strings.Join(parts, ",")
	default:
		return ""
	}
}

func normalizeValidity(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "unknown", nil
	}
	switch value {
	case "active", "historical", "rejected", "superseded", "stale", "unknown", "tombstoned":
		return value, nil
	default:
		return "", fmt.Errorf("invalid validity %q", value)
	}
}

func normalizeSourceKind(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "direct"
	}
	value = strings.ReplaceAll(value, "-", "_")
	switch value {
	case "transcript", "transcripts", "session", "session_evidence":
		return "session_evidence"
	case "project_file", "file_chunk":
		return "file"
	case "extracted", "claim":
		return "extracted_claim"
	default:
		return value
	}
}

func emptyToNull(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func (s *Store) tune() {
	s.db.Exec("PRAGMA cache_size = -64000")
	s.db.Exec("PRAGMA mmap_size = 268435456")
	s.db.Exec("PRAGMA temp_store = MEMORY")
}

func tightenStorePermissions(dbPath string) {
	_ = os.Chmod(dbPath, 0o600)
	_ = os.Chmod(dbPath+"-wal", 0o600)
	_ = os.Chmod(dbPath+"-shm", 0o600)
}

func formatSQLiteFeatureError(err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(strings.ToLower(err.Error()), "fts5") ||
		strings.Contains(strings.ToLower(err.Error()), "no such module") {
		return fmt.Errorf("sqlite FTS5 is required; build with CGO enabled and CGO_CFLAGS=\"-DSQLITE_ENABLE_FTS5\": %w", err)
	}
	return err
}

func memoryHash(p AddMemoryParams) string {
	h := sha256.New()
	parts := []string{
		p.Role,
		p.Content,
		p.SourceAgent,
		p.SourcePath,
		p.SourceRef,
		p.ScopeKind,
		p.ScopeID,
		p.ProjectID,
		p.SessionID,
		p.Room,
		p.MetadataJSON,
	}
	for _, part := range parts {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func publicID(hash string) string {
	raw, err := hex.DecodeString(hash)
	if err != nil {
		return "mem_" + hash[:20]
	}
	id := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw)
	return "mem_" + strings.ToLower(id[:26])
}

var ftsTermRE = regexp.MustCompile(`[A-Za-z0-9_]+`)

func FTSQuery(query string) string {
	terms := ftsTermRE.FindAllString(query, -1)
	if len(terms) == 0 {
		return ""
	}
	seen := make(map[string]bool, len(terms))
	out := make([]string, 0, len(terms))
	for _, term := range terms {
		term = strings.ToLower(term)
		if seen[term] {
			continue
		}
		seen[term] = true
		out = append(out, term)
	}
	return strings.Join(out, " OR ")
}
