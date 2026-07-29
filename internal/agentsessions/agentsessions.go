// Package agentsessions discovers and reads on-disk session transcripts from
// other coding agents (Claude Code, Codex, ...) so their durable evidence can
// be backfilled into recoil's session-evidence tier.
//
// Each agent stores transcripts differently, and — critically — binds a
// transcript to a repo differently: Claude Code path-encodes the cwd into a
// directory name, while Codex date-partitions files and records the cwd only
// inside the session_meta record. An adapter hides that asymmetry behind two
// methods: Discover (find this repo's sessions) and Read (normalize a session
// to the Turn model the selector already consumes).
package agentsessions

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/1broseidon/recoil/internal/sessionevidence"
)

// maxLineBytes bounds how large a single JSONL record we will parse. Agent
// transcripts occasionally carry a pasted file or base64 image on one line
// (the observed corpus has a 33MB line); such records are drained and skipped
// rather than allowed to abort a whole session or balloon memory.
const maxLineBytes = 8 * 1024 * 1024

// forEachJSONLine calls fn for each non-blank newline-delimited record in r,
// with bounded memory. A record longer than maxLineBytes is skipped, and a
// malformed record is fn's problem, not a fatal error — so one pathological
// line never aborts a backfill. fn returns false to stop early. The returned
// error reports only an underlying read fault. The slice passed to fn aliases
// an internal buffer and must not be retained past the call.
func forEachJSONLine(r io.Reader, fn func(line []byte) bool) error {
	br := bufio.NewReaderSize(r, maxLineBytes)
	for {
		line, err := br.ReadSlice('\n')
		if err == bufio.ErrBufferFull {
			// Oversized record: drain to end-of-line, then skip it.
			for err == bufio.ErrBufferFull {
				_, err = br.ReadSlice('\n')
			}
			if err == nil {
				continue
			}
			if err == io.EOF {
				return nil
			}
			return err
		}
		if trimmed := bytes.TrimSpace(line); len(trimmed) > 0 {
			if !fn(trimmed) {
				return nil
			}
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

// SessionRef identifies one discovered session bound to a repo. It carries
// enough provenance to ingest the session and to skip it on a later run.
type SessionRef struct {
	Agent           string    `json:"agent"`
	SessionID       string    `json:"session_id"`
	NativeSessionID string    `json:"native_session_id,omitempty"`
	Path            string    `json:"path"`
	Repo            string    `json:"repo"`
	Branch          string    `json:"branch,omitempty"`
	ModTime         time.Time `json:"mod_time"`
	Size            int64     `json:"size"`
}

// Adapter knows how to find and read one agent's sessions.
type Adapter interface {
	// Name is the source_agent stamped on ingested evidence (e.g. "claude").
	Name() string
	// Discover returns the sessions bound to repoRoot, and only those. An
	// adapter MUST verify the repo binding from inside each transcript, never
	// trust the path alone — otherwise it leaks other repos' chats.
	Discover(repoRoot string) ([]SessionRef, error)
	// Read normalizes a discovered session into selector-ready turns.
	Read(ref SessionRef) ([]sessionevidence.Turn, error)
}

func allAdapters() []Adapter {
	return []Adapter{claudeAdapter{}, codexAdapter{}}
}

// Adapters returns the requested adapters by name, or all of them when no
// names are given. Unknown names are an error so a typo never silently
// backfills nothing.
func Adapters(names ...string) ([]Adapter, error) {
	if len(names) == 0 {
		return allAdapters(), nil
	}
	byName := map[string]Adapter{}
	for _, a := range allAdapters() {
		byName[a.Name()] = a
	}
	var out []Adapter
	for _, name := range names {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" {
			continue
		}
		a, ok := byName[name]
		if !ok {
			return nil, fmt.Errorf("unknown session agent %q (known: claude, codex)", name)
		}
		out = append(out, a)
	}
	if len(out) == 0 {
		return allAdapters(), nil
	}
	return out, nil
}

// cleanPath normalizes a path for repo-binding equality. Symlinks are
// resolved when possible so /home vs a realpath spelling still match.
func cleanPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	p = filepath.Clean(p)
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	return p
}

// Cursors records the (mod-time, size) of each session at last backfill so a
// re-run skips unchanged transcripts. Session logs are append-only, so a grown
// file simply re-ingests (content dedup in the store absorbs the overlap).
type Cursors struct {
	path    string
	Entries map[string]cursorEntry `json:"entries"`
}

type cursorEntry struct {
	SessionID       string `json:"session_id"`
	NativeSessionID string `json:"native_session_id,omitempty"`
	Path            string `json:"path,omitempty"`
	ModTime         string `json:"mod_time"`
	Size            int64  `json:"size"`
	Selected        int    `json:"selected"`
	IngestedAt      string `json:"ingested_at"`
}

func cursorKey(agent, nativeSessionID string) string { return agent + "|" + nativeSessionID }

func sessionNativeID(ref SessionRef) string {
	if strings.TrimSpace(ref.NativeSessionID) != "" {
		return strings.TrimSpace(ref.NativeSessionID)
	}
	if strings.TrimSpace(ref.SessionID) != "" {
		return strings.TrimSpace(ref.SessionID)
	}
	return strings.TrimSpace(ref.Path)
}

// LoadCursors reads the backfill cursor file for a scope, co-located with the
// scope's compact evidence so `session-evidence forget` semantics stay simple.
func LoadCursors(stateDir, scopeID string) (*Cursors, error) {
	dir, err := sessionevidence.ScopeDir(stateDir, scopeID)
	if err != nil {
		return nil, err
	}
	c := &Cursors{path: filepath.Join(dir, ".backfill-cursors.json"), Entries: map[string]cursorEntry{}}
	data, err := os.ReadFile(c.path)
	if err != nil {
		if os.IsNotExist(err) {
			return c, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, c); err != nil {
		// A corrupt cursor file should not block a backfill; start fresh.
		c.Entries = map[string]cursorEntry{}
	}
	if c.Entries == nil {
		c.Entries = map[string]cursorEntry{}
	}
	return c, nil
}

// Unchanged reports whether ref was already ingested at its current size and
// mod-time.
func (c *Cursors) Unchanged(ref SessionRef) bool {
	entry, ok := c.Entries[cursorKey(ref.Agent, sessionNativeID(ref))]
	if !ok {
		if entry, ok = c.Entries[cursorKey(ref.Agent, ref.Path)]; !ok {
			return false
		}
	}
	return entry.Size == ref.Size && entry.ModTime == ref.ModTime.UTC().Format(time.RFC3339)
}

// SeenNative reports whether this agent/native session id has already been
// captured, regardless of cursor size. Backfill uses this to avoid duplicating
// sessions that the live hook already wrote under a different compact file.
func (c *Cursors) SeenNative(ref SessionRef) bool {
	_, ok := c.Entries[cursorKey(ref.Agent, sessionNativeID(ref))]
	return ok
}

// Record marks ref as ingested.
func (c *Cursors) Record(ref SessionRef, selected int, now time.Time) {
	c.Entries[cursorKey(ref.Agent, sessionNativeID(ref))] = cursorEntry{
		SessionID:       ref.SessionID,
		NativeSessionID: sessionNativeID(ref),
		Path:            ref.Path,
		ModTime:         ref.ModTime.UTC().Format(time.RFC3339),
		Size:            ref.Size,
		Selected:        selected,
		IngestedAt:      now.UTC().Format(time.RFC3339),
	}
}

// Save persists the cursor file, creating the scope dir if needed.
func (c *Cursors) Save() error {
	if c.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.path, data, 0o600)
}

// sortRefs orders sessions oldest-first so backfill ingests history in the
// order it happened.
func sortRefs(refs []SessionRef) {
	sort.SliceStable(refs, func(i, j int) bool {
		return refs[i].ModTime.Before(refs[j].ModTime)
	})
}
