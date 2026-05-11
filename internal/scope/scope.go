package scope

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/1broseidon/recoil/internal/config"
)

const (
	ProjectDirName  = ".recoil"
	ProjectFileName = "project.json"
)

type Scope struct {
	Kind        string
	ID          string
	ProjectID   string
	SessionID   string
	Portable    bool
	Root        string
	Initialized bool
	MarkerPath  string
}

type ProjectFile struct {
	Version   string `json:"version"`
	ProjectID string `json:"project_id"`
	CreatedAt string `json:"created_at"`
}

func UserScope() (Scope, error) {
	dir, err := config.ResolveStateDir()
	if err != nil {
		return Scope{}, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Scope{}, err
	}
	path := filepath.Join(dir, "user_id")
	if b, err := os.ReadFile(path); err == nil {
		id := strings.TrimSpace(string(b))
		if id != "" {
			return Scope{Kind: "user", ID: id, Portable: true}, nil
		}
	}
	id, err := newUUID()
	if err != nil {
		return Scope{}, err
	}
	if err := os.WriteFile(path, []byte(id+"\n"), 0o600); err != nil {
		return Scope{}, err
	}
	return Scope{Kind: "user", ID: id, Portable: true}, nil
}

func ProjectScope(workspace string) (Scope, error) {
	if workspace == "" {
		workspace = "."
	}
	start, err := canonicalDir(workspace)
	if err != nil {
		return Scope{}, err
	}

	if root, marker, ok, err := FindProjectRoot(start); err != nil {
		return Scope{}, err
	} else if ok {
		id := projectIDFromMarker(marker)
		portable := strings.HasPrefix(id, "git:")
		if id == "" {
			id, portable = computedProjectID(root)
		}
		return Scope{
			Kind:        "project",
			ID:          id,
			ProjectID:   id,
			Portable:    portable,
			Root:        root,
			Initialized: true,
			MarkerPath:  marker,
		}, nil
	}

	root := gitOutput(start, "rev-parse", "--show-toplevel")
	if root == "" {
		id := "local:" + hashString(start)
		return Scope{Kind: "project", ID: id, ProjectID: id, Root: start}, nil
	}
	root = strings.TrimSpace(root)
	id, portable := computedProjectID(root)
	return Scope{Kind: "project", ID: id, ProjectID: id, Portable: portable, Root: root}, nil
}

func InitProject(workspace string) (Scope, error) {
	sc, err := ProjectScope(workspace)
	if err != nil {
		return Scope{}, err
	}
	if sc.Initialized {
		return sc, nil
	}
	root := sc.Root
	if root == "" {
		root, err = canonicalDir(workspace)
		if err != nil {
			return Scope{}, err
		}
	}
	projectDir := filepath.Join(root, ProjectDirName)
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		return Scope{}, err
	}
	marker := filepath.Join(projectDir, ProjectFileName)
	payload := ProjectFile{
		Version:   "0.1",
		ProjectID: sc.ProjectID,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return Scope{}, err
	}
	data = append(data, '\n')
	if err := os.WriteFile(marker, data, 0o600); err != nil {
		return Scope{}, err
	}
	sc.Initialized = true
	sc.MarkerPath = marker
	return sc, nil
}

func FindProjectRoot(workspace string) (root, marker string, ok bool, err error) {
	dir, err := canonicalDir(workspace)
	if err != nil {
		return "", "", false, err
	}
	for {
		marker := filepath.Join(dir, ProjectDirName, ProjectFileName)
		if _, err := os.Stat(marker); err == nil {
			return dir, marker, true, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", "", false, nil
		}
		dir = parent
	}
}

func computedProjectID(root string) (string, bool) {
	remote := gitOutput(root, "config", "--get", "remote.origin.url")
	if remote != "" {
		id := "git:" + hashString(strings.TrimSpace(remote))
		return id, true
	}
	id := "local:" + hashString(root)
	return id, false
}

func projectIDFromMarker(marker string) string {
	data, err := os.ReadFile(marker)
	if err != nil {
		return ""
	}
	var pf ProjectFile
	if err := json.Unmarshal(data, &pf); err != nil {
		return ""
	}
	return strings.TrimSpace(pf.ProjectID)
}

func canonicalDir(path string) (string, error) {
	if path == "" {
		path = "."
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if info, err := os.Stat(abs); err == nil && !info.IsDir() {
		abs = filepath.Dir(abs)
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	return abs, nil
}

func SessionScope(sessionID string) (Scope, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return Scope{}, fmt.Errorf("session ID is empty")
	}
	return Scope{Kind: "session", ID: sessionID, SessionID: sessionID, Portable: true}, nil
}

func gitOutput(dir string, args ...string) string {
	allArgs := append([]string{"-C", dir}, args...)
	cmd := exec.Command("git", allArgs...)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func hashString(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:32]
}

func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
