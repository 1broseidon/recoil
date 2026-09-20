package traystats

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/1broseidon/recoil/internal/config"
	"github.com/1broseidon/recoil/internal/scope"
	_ "github.com/mattn/go-sqlite3"
)

var ErrMissingDB = errors.New("recoil database not found")

type ResolveOptions struct {
	DBPath     string
	Project    string
	AllScopes  bool
	WorkingDir string
}

type Target struct {
	DBPath             string
	ScopeKind          string
	ScopeID            string
	ProjectRoot        string
	ProjectName        string
	ProjectInitialized bool
	AllScopes          bool
}

type CollectOptions struct {
	Now             time.Time
	AgentSessionTTL time.Duration
}

type Snapshot struct {
	DBPath             string
	Ready              bool
	Message            string
	ScopeKind          string
	ScopeID            string
	ProjectRoot        string
	ProjectName        string
	ProjectInitialized bool
	AllScopes          bool
	TotalCount         int
	ActiveCount        int
	CurrentCount       int
	StaleCount         int
	HistoricalCount    int
	TombstoneCount     int
	Added24h           int
	SourceCount        int
	ActiveSessions     int
	LastActivityAt     string
}

func ResolveTarget(opts ResolveOptions) (Target, error) {
	dbPath, err := config.ResolveDBPath(opts.DBPath)
	if err != nil {
		return Target{}, err
	}
	target := Target{
		DBPath:    dbPath,
		AllScopes: opts.AllScopes,
	}
	if opts.AllScopes {
		target.ProjectName = "all scopes"
		return target, nil
	}

	projectPath := strings.TrimSpace(opts.Project)
	if projectPath == "" {
		projectPath = strings.TrimSpace(opts.WorkingDir)
	}
	if projectPath == "" {
		projectPath = "."
	}
	sc, err := scope.ProjectScope(projectPath)
	if err != nil {
		return Target{}, err
	}
	target.ScopeKind = sc.Kind
	target.ScopeID = sc.ID
	target.ProjectRoot = sc.Root
	target.ProjectInitialized = sc.Initialized
	target.ProjectName = projectDisplayName(sc)

	if !dbPathExplicit(opts.DBPath) && sc.Initialized && sc.Root != "" && !config.PathExists(dbPath) {
		projectDBPath := filepath.Join(sc.Root, scope.ProjectDirName, "recoil.db")
		if config.PathExists(projectDBPath) {
			target.DBPath = projectDBPath
		}
	}
	return target, nil
}

func Collect(ctx context.Context, target Target, opts CollectOptions) (Snapshot, error) {
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	ttl := opts.AgentSessionTTL
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	snapshot := Snapshot{
		DBPath:             target.DBPath,
		ScopeKind:          target.ScopeKind,
		ScopeID:            target.ScopeID,
		ProjectRoot:        target.ProjectRoot,
		ProjectName:        target.ProjectName,
		ProjectInitialized: target.ProjectInitialized,
		AllScopes:          target.AllScopes,
	}
	if strings.TrimSpace(target.DBPath) == "" {
		snapshot.Message = "database path is empty"
		return snapshot, nil
	}
	if _, err := os.Stat(target.DBPath); err != nil {
		if os.IsNotExist(err) {
			snapshot.Message = ErrMissingDB.Error()
			return snapshot, nil
		}
		return snapshot, err
	}

	db, err := openReadOnly(target.DBPath)
	if err != nil {
		snapshot.Message = err.Error()
		return snapshot, nil
	}
	defer db.Close()

	if ok, err := tableExists(ctx, db, "memories"); err != nil {
		return snapshot, err
	} else if !ok {
		snapshot.Message = "database is not initialized"
		return snapshot, nil
	}

	if err := collectMemoryCounts(ctx, db, target, now, &snapshot); err != nil {
		return snapshot, err
	}
	sourceCount, err := collectSourceCount(ctx, db, target)
	if err != nil {
		return snapshot, err
	}
	snapshot.SourceCount = sourceCount
	activeSessions, err := collectActiveSessions(ctx, db, target, now.Add(-ttl))
	if err != nil {
		return snapshot, err
	}
	snapshot.ActiveSessions = activeSessions
	snapshot.Ready = true
	snapshot.Message = "ready"
	return snapshot, nil
}

func openReadOnly(path string) (*sql.DB, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	dsn := fmt.Sprintf("file:%s?mode=ro&_busy_timeout=5000", url.PathEscape(abs))
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func collectMemoryCounts(ctx context.Context, db *sql.DB, target Target, now time.Time, snapshot *Snapshot) error {
	where, args := scopedWhere(target)
	args = append([]any{now.UTC().Add(-24 * time.Hour).Format(time.RFC3339)}, args...)
	query := `
SELECT
	COUNT(*),
	COALESCE(SUM(CASE WHEN tombstoned_at IS NULL THEN 1 ELSE 0 END), 0),
	COALESCE(SUM(CASE
		WHEN tombstoned_at IS NULL
			AND COALESCE(validity, 'unknown') NOT IN ('historical', 'rejected', 'superseded', 'stale', 'tombstoned')
		THEN 1 ELSE 0 END), 0),
	COALESCE(SUM(CASE
		WHEN tombstoned_at IS NULL AND COALESCE(validity, 'unknown') = 'stale'
		THEN 1 ELSE 0 END), 0),
	COALESCE(SUM(CASE
		WHEN tombstoned_at IS NULL
			AND COALESCE(validity, 'unknown') IN ('historical', 'rejected', 'superseded', 'stale', 'tombstoned')
		THEN 1 ELSE 0 END), 0),
	COALESCE(SUM(CASE WHEN tombstoned_at IS NOT NULL THEN 1 ELSE 0 END), 0),
	COALESCE(SUM(CASE
		WHEN tombstoned_at IS NULL AND created_at >= ?
		THEN 1 ELSE 0 END), 0),
	COALESCE(MAX(COALESCE(tombstoned_at, created_at)), '')
FROM memories
WHERE 1=1` + where
	return db.QueryRowContext(ctx, query, args...).Scan(
		&snapshot.TotalCount,
		&snapshot.ActiveCount,
		&snapshot.CurrentCount,
		&snapshot.StaleCount,
		&snapshot.HistoricalCount,
		&snapshot.TombstoneCount,
		&snapshot.Added24h,
		&snapshot.LastActivityAt,
	)
}

func collectSourceCount(ctx context.Context, db *sql.DB, target Target) (int, error) {
	if ok, err := tableExists(ctx, db, "sources"); err != nil {
		return 0, err
	} else if !ok {
		return 0, nil
	}
	where, args := scopedWhere(target)
	var count int
	err := db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM sources
WHERE deleted_at IS NULL`+where, args...).Scan(&count)
	return count, err
}

func collectActiveSessions(ctx context.Context, db *sql.DB, target Target, cutoff time.Time) (int, error) {
	if ok, err := tableExists(ctx, db, "agent_sessions"); err != nil {
		return 0, err
	} else if !ok {
		return 0, nil
	}
	where, args := scopedWhere(target)
	args = append(args, cutoff.UTC().Format(time.RFC3339))
	var count int
	err := db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM agent_sessions
WHERE 1=1`+where+`
	AND last_seen_at >= ?`, args...).Scan(&count)
	return count, err
}

func tableExists(ctx context.Context, db *sql.DB, name string) (bool, error) {
	var found string
	err := db.QueryRowContext(ctx, `
SELECT name
FROM sqlite_master
WHERE type IN ('table', 'view')
	AND name = ?
LIMIT 1`, name).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func scopedWhere(target Target) (string, []any) {
	if target.AllScopes || target.ScopeKind == "" || target.ScopeID == "" {
		return "", nil
	}
	return " AND scope_kind = ? AND scope_id = ?", []any{target.ScopeKind, target.ScopeID}
}

func dbPathExplicit(flagPath string) bool {
	return strings.TrimSpace(flagPath) != "" || strings.TrimSpace(os.Getenv("RECOIL_DB")) != ""
}

func projectDisplayName(sc scope.Scope) string {
	if sc.Root != "" {
		if name := filepath.Base(sc.Root); name != "." && name != string(filepath.Separator) {
			return name
		}
	}
	if sc.ID != "" {
		return sc.ID
	}
	return "project"
}
