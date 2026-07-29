package agentsessions

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/1broseidon/recoil/internal/sessionevidence"
)

// codexAdapter reads Codex rollout transcripts from
// ~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl. The files are partitioned by
// date, not by repo, so the repo binding lives only inside the session_meta
// (and turn_context) records as payload.cwd. Discovery therefore must scan and
// filter — a naive "ingest the sessions dir" would leak every other repo's
// chats into this repo's memory.
type codexAdapter struct{}

func (codexAdapter) Name() string { return "codex" }

type codexEnvelope struct {
	Type      string          `json:"type"`
	Timestamp string          `json:"timestamp"`
	Payload   json.RawMessage `json:"payload"`
}

type codexMeta struct {
	ID  string `json:"id"`
	Cwd string `json:"cwd"`
	Git *struct {
		Branch string `json:"branch"`
	} `json:"git"`
}

type codexItem struct {
	Type    string `json:"type"`
	Role    string `json:"role"`
	Content any    `json:"content"`
}

func codexSessionsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex", "sessions"), nil
}

func (codexAdapter) Discover(repoRoot string) ([]SessionRef, error) {
	root, err := codexSessionsDir()
	if err != nil {
		return nil, err
	}
	repoRoot = cleanPath(repoRoot)
	if repoRoot == "" {
		return nil, nil
	}

	var refs []SessionRef
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries rather than abort the sweep
		}
		if d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		cwd, sid, branch, ok := codexFileBinding(path)
		if !ok || cleanPath(cwd) != repoRoot {
			return nil
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil
		}
		if sid == "" {
			sid = strings.TrimSuffix(filepath.Base(path), ".jsonl")
		}
		refs = append(refs, SessionRef{
			Agent:           "codex",
			SessionID:       sid,
			NativeSessionID: sid,
			Path:            path,
			Repo:            repoRoot,
			Branch:          branch,
			ModTime:         info.ModTime().UTC(),
			Size:            info.Size(),
		})
		return nil
	})
	if walkErr != nil && !os.IsNotExist(walkErr) {
		return refs, walkErr
	}
	sortRefs(refs)
	return refs, nil
}

// codexFileBinding reads the leading records until it finds the cwd, which the
// session_meta record carries at the top of the file.
func codexFileBinding(path string) (cwd, sessionID, branch string, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", "", false
	}
	defer f.Close()
	lines := 0
	_ = forEachJSONLine(f, func(line []byte) bool {
		lines++
		if lines > 200 {
			return false
		}
		var env codexEnvelope
		if json.Unmarshal(line, &env) != nil {
			return true
		}
		if env.Type != "session_meta" && env.Type != "turn_context" {
			return true
		}
		var meta codexMeta
		if json.Unmarshal(env.Payload, &meta) != nil {
			return true
		}
		if strings.TrimSpace(meta.Cwd) == "" {
			return true
		}
		cwd = strings.TrimSpace(meta.Cwd)
		sessionID = strings.TrimSpace(meta.ID)
		if meta.Git != nil {
			branch = strings.TrimSpace(meta.Git.Branch)
		}
		ok = true
		return false
	})
	return cwd, sessionID, branch, ok
}

func (codexAdapter) Read(ref SessionRef) ([]sessionevidence.Turn, error) {
	f, err := os.Open(ref.Path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var turns []sessionevidence.Turn
	err = forEachJSONLine(f, func(line []byte) bool {
		var env codexEnvelope
		if json.Unmarshal(line, &env) != nil {
			return true
		}
		if env.Type != "response_item" {
			return true
		}
		var item codexItem
		if json.Unmarshal(env.Payload, &item) != nil {
			return true
		}
		role := strings.ToLower(strings.TrimSpace(item.Role))
		if role != "user" && role != "assistant" {
			return true
		}
		content := strings.TrimSpace(sessionevidence.ContentString(item.Content))
		if content == "" || isCodexInjectedContext(content) {
			return true
		}
		turns = append(turns, sessionevidence.Turn{
			Index:     len(turns) + 1,
			Role:      role,
			Content:   content,
			Timestamp: strings.TrimSpace(env.Timestamp),
		})
		return true
	})
	return turns, err
}

// isCodexInjectedContext drops the synthetic environment/context blocks Codex
// prepends to user turns; they are harness scaffolding, not authored intent.
func isCodexInjectedContext(content string) bool {
	switch {
	case strings.HasPrefix(content, "<environment_context>"):
		return true
	case strings.HasPrefix(content, "<user_instructions>"):
		return true
	default:
		return false
	}
}
