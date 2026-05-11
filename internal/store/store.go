package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/1broseidon/recoil/internal/redact"
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
	SourceAgent  string  `json:"source_agent,omitempty"`
	SourcePath   string  `json:"source_path,omitempty"`
	SourceRef    string  `json:"source_ref,omitempty"`
	ScopeKind    string  `json:"scope_kind"`
	ScopeID      string  `json:"scope_id"`
	ProjectID    string  `json:"project_id,omitempty"`
	SessionID    string  `json:"session_id,omitempty"`
	Room         string  `json:"room,omitempty"`
	MetadataJSON string  `json:"metadata_json,omitempty"`
	CreatedAt    string  `json:"created_at"`
	TombstonedAt string  `json:"tombstoned_at,omitempty"`
	Score        float64 `json:"score,omitempty"`
	Excerpt      string  `json:"excerpt,omitempty"`
}

type AddMemoryParams struct {
	Role         string
	Content      string
	SourceAgent  string
	SourcePath   string
	SourceRef    string
	ScopeKind    string
	ScopeID      string
	ProjectID    string
	SessionID    string
	Room         string
	MetadataJSON string
}

type SearchParams struct {
	Query       string
	ScopeKind   string
	ScopeID     string
	SourceAgent string
	SourcePath  string
	Since       string
	Before      string
	Limit       int
}

type ListParams struct {
	ScopeKind      string
	ScopeID        string
	SourceAgent    string
	SourcePath     string
	Since          string
	Before         string
	Limit          int
	IncludeDeleted bool
}

type ForgetParams struct {
	IDOrPrefix string
	Reason     string
	Destroy    bool
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

func (s *Store) AddMemory(ctx context.Context, p AddMemoryParams) (*Memory, bool, error) {
	p.Content = redact.Content(p.Content)
	p.ScopeKind = strings.TrimSpace(p.ScopeKind)
	p.ScopeID = strings.TrimSpace(p.ScopeID)
	if strings.TrimSpace(p.Content) == "" {
		return nil, false, fmt.Errorf("memory content is empty")
	}
	if p.ScopeKind == "" || p.ScopeID == "" {
		return nil, false, fmt.Errorf("memory scope is required")
	}

	hash := memoryHash(p)
	id := publicID(hash)
	now := time.Now().UTC().Format(time.RFC3339)

	res, err := s.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO memories (
			id, hash, role, content, source_agent, source_path, source_ref,
			scope_kind, scope_id, project_id, session_id, room, metadata_json, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, hash, p.Role, p.Content, p.SourceAgent, p.SourcePath, p.SourceRef,
		p.ScopeKind, p.ScopeID, p.ProjectID, p.SessionID, p.Room, p.MetadataJSON, now,
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
	query := FTSQuery(p.Query)
	if query == "" {
		return nil, nil
	}
	if p.Limit <= 0 {
		p.Limit = 5
	}
	if p.Limit > 100 {
		p.Limit = 100
	}
	where, args := scopedFilter("m", p.ScopeKind, p.ScopeID, p.SourceAgent, p.SourcePath, p.Since, p.Before, false)
	args = append([]any{query}, args...)
	args = append(args, "%"+strings.ToLower(strings.TrimSpace(p.Query))+"%", p.Limit)
	sqlText := `
		SELECT
			m.id, m.hash, m.role, m.content, m.source_agent, m.source_path, m.source_ref,
			m.scope_kind, m.scope_id, m.project_id, m.session_id, m.room, m.metadata_json,
			m.created_at, COALESCE(m.tombstoned_at, ''),
			bm25(memories_fts) AS rank,
			snippet(memories_fts, 0, '', '', '...', 18) AS excerpt
		FROM memories_fts
		JOIN memories m ON m.pk = memories_fts.rowid
		WHERE memories_fts MATCH ?` + where + `
		ORDER BY CASE WHEN lower(m.content) LIKE ? THEN rank - 1.0 ELSE rank END
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
			&mem.ID, &mem.Hash, &mem.Role, &mem.Content, &mem.SourceAgent, &mem.SourcePath, &mem.SourceRef,
			&mem.ScopeKind, &mem.ScopeID, &mem.ProjectID, &mem.SessionID, &mem.Room, &mem.MetadataJSON,
			&mem.CreatedAt, &mem.TombstonedAt, &rank, &mem.Excerpt,
		); err != nil {
			return nil, err
		}
		mem.Score = retrievalScore(rank)
		results = append(results, mem)
	}
	return results, rows.Err()
}

func retrievalScore(rank float64) float64 {
	absRank := math.Abs(rank)
	return absRank / (1 + absRank)
}

func (s *Store) List(ctx context.Context, p ListParams) ([]Memory, error) {
	if p.Limit <= 0 {
		p.Limit = 50
	}
	if p.Limit > 1000 {
		p.Limit = 1000
	}
	where, args := scopedFilter("", p.ScopeKind, p.ScopeID, p.SourceAgent, p.SourcePath, p.Since, p.Before, p.IncludeDeleted)
	args = append(args, p.Limit)
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, hash, role, content, source_agent, source_path, source_ref,
			scope_kind, scope_id, project_id, session_id, room, metadata_json,
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

func (s *Store) GetMemoryByID(ctx context.Context, id string, includeDeleted bool) (*Memory, error) {
	deletedClause := "AND tombstoned_at IS NULL"
	if includeDeleted {
		deletedClause = ""
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT id, hash, role, content, source_agent, source_path, source_ref,
			scope_kind, scope_id, project_id, session_id, room, metadata_json,
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

func (s *Store) memoryByHash(ctx context.Context, hash string) (*Memory, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, hash, role, content, source_agent, source_path, source_ref,
			scope_kind, scope_id, project_id, session_id, room, metadata_json,
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
		&mem.ID, &mem.Hash, &mem.Role, &mem.Content, &mem.SourceAgent, &mem.SourcePath, &mem.SourceRef,
		&mem.ScopeKind, &mem.ScopeID, &mem.ProjectID, &mem.SessionID, &mem.Room, &mem.MetadataJSON,
		&mem.CreatedAt, &mem.TombstonedAt,
	)
	return mem, err
}

func scopedFilter(alias, scopeKind, scopeID, sourceAgent, sourcePath, since, before string, includeDeleted bool) (string, []any) {
	col := func(name string) string {
		if alias == "" {
			return name
		}
		return alias + "." + name
	}
	var where strings.Builder
	var args []any
	if !includeDeleted {
		fmt.Fprintf(&where, "\n\t\t\tAND %s IS NULL", col("tombstoned_at"))
	}
	if scopeKind != "" {
		fmt.Fprintf(&where, "\n\t\t\tAND %s = ?", col("scope_kind"))
		args = append(args, scopeKind)
	}
	if scopeID != "" {
		fmt.Fprintf(&where, "\n\t\t\tAND %s = ?", col("scope_id"))
		args = append(args, scopeID)
	}
	if sourceAgent != "" {
		fmt.Fprintf(&where, "\n\t\t\tAND %s = ?", col("source_agent"))
		args = append(args, sourceAgent)
	}
	if sourcePath != "" {
		fmt.Fprintf(&where, "\n\t\t\tAND %s LIKE ?", col("source_path"))
		args = append(args, "%"+sourcePath+"%")
	}
	if since != "" {
		fmt.Fprintf(&where, "\n\t\t\tAND %s >= ?", col("created_at"))
		args = append(args, since)
	}
	if before != "" {
		fmt.Fprintf(&where, "\n\t\t\tAND %s <= ?", col("created_at"))
		args = append(args, before)
	}
	return where.String(), args
}

func (s *Store) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS memories (
			pk INTEGER PRIMARY KEY AUTOINCREMENT,
			id TEXT NOT NULL UNIQUE,
			hash TEXT NOT NULL UNIQUE,
			role TEXT,
			content TEXT NOT NULL,
			source_agent TEXT,
			source_path TEXT,
			source_ref TEXT,
			scope_kind TEXT NOT NULL,
			scope_id TEXT NOT NULL,
			project_id TEXT,
			session_id TEXT,
			room TEXT,
			metadata_json TEXT,
			created_at TEXT NOT NULL,
			tombstoned_at TEXT,
			purge_reason TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_memories_scope ON memories(scope_kind, scope_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_memories_created ON memories(created_at)`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS memories_fts USING fts5(
			content,
			content='memories',
			content_rowid='pk'
		)`,
		`CREATE TRIGGER IF NOT EXISTS memories_ai AFTER INSERT ON memories BEGIN
			INSERT INTO memories_fts(rowid, content) VALUES (new.pk, new.content);
		END`,
		`CREATE TRIGGER IF NOT EXISTS memories_ad AFTER DELETE ON memories BEGIN
			INSERT INTO memories_fts(memories_fts, rowid, content) VALUES('delete', old.pk, old.content);
		END`,
		`CREATE TRIGGER IF NOT EXISTS memories_au AFTER UPDATE OF content ON memories BEGIN
			INSERT INTO memories_fts(memories_fts, rowid, content) VALUES('delete', old.pk, old.content);
			INSERT INTO memories_fts(rowid, content) VALUES (new.pk, new.content);
		END`,
		`CREATE TABLE IF NOT EXISTS sources (
			id TEXT PRIMARY KEY,
			kind TEXT NOT NULL,
			path TEXT,
			agent TEXT,
			last_mined_at TEXT,
			metadata_json TEXT
		)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return formatSQLiteFeatureError(err)
		}
	}
	if _, err := s.db.Exec(`INSERT INTO memories_fts(memories_fts) VALUES('rebuild')`); err != nil {
		return formatSQLiteFeatureError(err)
	}
	return nil
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
