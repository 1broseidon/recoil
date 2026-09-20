package agentsessions

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/1broseidon/recoil/internal/sessionevidence"
)

// claudeAdapter reads Claude Code transcripts from
// ~/.claude/projects/<encoded-cwd>/<sessionId>.jsonl. Each line is a typed
// event; user/assistant events carry a nested `message` and the originating
// `cwd`, which we use to confirm the repo binding.
type claudeAdapter struct{}

func (claudeAdapter) Name() string { return "claude" }

type claudeEvent struct {
	Type        string `json:"type"`
	Timestamp   string `json:"timestamp"`
	Cwd         string `json:"cwd"`
	GitBranch   string `json:"gitBranch"`
	SessionID   string `json:"sessionId"`
	IsSidechain bool   `json:"isSidechain"`
	IsMeta      bool   `json:"isMeta"`
	Message     *struct {
		Role    string `json:"role"`
		Content any    `json:"content"`
	} `json:"message"`
}

func claudeProjectsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "projects"), nil
}

// encodeClaudePath mirrors Claude Code's directory encoding (every
// non-alphanumeric rune becomes '-'). It is only a fast path to the right
// directory; Discover still verifies the cwd recorded inside each file, so a
// mismatched encoding degrades to a full scan rather than wrong results.
func encodeClaudePath(p string) string {
	var b strings.Builder
	for _, r := range p {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

func (claudeAdapter) Discover(repoRoot string) ([]SessionRef, error) {
	root, err := claudeProjectsDir()
	if err != nil {
		return nil, err
	}
	repoRoot = cleanPath(repoRoot)
	if repoRoot == "" {
		return nil, nil
	}

	var dirs []string
	encoded := filepath.Join(root, encodeClaudePath(repoRoot))
	if fi, err := os.Stat(encoded); err == nil && fi.IsDir() {
		dirs = []string{encoded}
	} else {
		entries, err := os.ReadDir(root)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, nil
			}
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() {
				dirs = append(dirs, filepath.Join(root, e.Name()))
			}
		}
	}

	var refs []SessionRef
	for _, dir := range dirs {
		files, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
		if err != nil {
			continue
		}
		for _, f := range files {
			cwd, branch, sid, ok := claudeFileBinding(f)
			if !ok || cleanPath(cwd) != repoRoot {
				continue
			}
			info, err := os.Stat(f)
			if err != nil {
				continue
			}
			if sid == "" {
				sid = strings.TrimSuffix(filepath.Base(f), ".jsonl")
			}
			refs = append(refs, SessionRef{
				Agent:           "claude",
				SessionID:       sid,
				NativeSessionID: sid,
				Path:            f,
				Repo:            repoRoot,
				Branch:          branch,
				ModTime:         info.ModTime().UTC(),
				Size:            info.Size(),
			})
		}
	}
	sortRefs(refs)
	return refs, nil
}

// claudeFileBinding reads only as far as the first event that records a cwd,
// so verifying a candidate is cheap even across unrelated projects.
func claudeFileBinding(path string) (cwd, branch, sessionID string, ok bool) {
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
		var ev claudeEvent
		if json.Unmarshal(line, &ev) != nil {
			return true
		}
		if sessionID == "" {
			sessionID = strings.TrimSpace(ev.SessionID)
		}
		if strings.TrimSpace(ev.Cwd) != "" {
			cwd = strings.TrimSpace(ev.Cwd)
			branch = strings.TrimSpace(ev.GitBranch)
			ok = true
			return false
		}
		return true
	})
	return cwd, branch, sessionID, ok
}

func (claudeAdapter) Read(ref SessionRef) ([]sessionevidence.Turn, error) {
	f, err := os.Open(ref.Path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var turns []sessionevidence.Turn
	err = forEachJSONLine(f, func(line []byte) bool {
		var ev claudeEvent
		if json.Unmarshal(line, &ev) != nil {
			return true
		}
		if ev.Type != "user" && ev.Type != "assistant" {
			return true
		}
		if ev.IsSidechain || ev.IsMeta || ev.Message == nil {
			return true
		}
		content := strings.TrimSpace(sessionevidence.ContentString(ev.Message.Content))
		if content == "" {
			return true
		}
		turns = append(turns, sessionevidence.Turn{
			Index:     len(turns) + 1,
			Role:      ev.Type,
			Content:   content,
			Timestamp: strings.TrimSpace(ev.Timestamp),
		})
		return true
	})
	return turns, err
}
