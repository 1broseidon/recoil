package sessionevidence

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/1broseidon/recoil/internal/redact"
)

const (
	SourceKind       = "session_evidence"
	DefaultMinChars  = 200
	minDirectiveSize = 24
	maxContentChars  = 2200
)

type Options struct {
	StateDir    string
	ScopeKind   string
	ScopeID     string
	SourceAgent string
	SessionID   string
	MinChars    int
	Now         time.Time
}

type Turn struct {
	Index     int
	Role      string
	Content   string
	Timestamp string
	ToolName  string
}

type Record struct {
	Version        int      `json:"version"`
	SessionID      string   `json:"session_id"`
	SourceAgent    string   `json:"source_agent"`
	ScopeKind      string   `json:"scope_kind"`
	ScopeID        string   `json:"scope_id"`
	TurnStart      int      `json:"turn_start"`
	TurnEnd        int      `json:"turn_end"`
	EvidenceType   string   `json:"evidence_type"`
	EvidenceTypes  []string `json:"evidence_types,omitempty"`
	SelectorReason string   `json:"selector_reason"`
	Timestamp      string   `json:"timestamp"`
	Redacted       bool     `json:"redacted"`
	Content        string   `json:"content"`
}

type File struct {
	Path       string
	SourcePath string
	SessionID  string
	Records    []Record
	Hash       string
	Size       int64
	ModTime    string
}

type IngestResult struct {
	Path       string   `json:"path,omitempty"`
	SourcePath string   `json:"source_path,omitempty"`
	SessionID  string   `json:"session_id"`
	Selected   int      `json:"selected"`
	Skipped    int      `json:"skipped"`
	Reasons    []string `json:"reasons,omitempty"`
}

type rawTranscript struct {
	SessionID string    `json:"session_id"`
	ID        string    `json:"id"`
	Turns     []rawTurn `json:"turns"`
	Messages  []rawTurn `json:"messages"`
	Items     []rawTurn `json:"items"`
	Events    []rawTurn `json:"events"`
}

type rawTurn struct {
	TurnIndex int         `json:"turn_index"`
	Index     int         `json:"index"`
	Role      string      `json:"role"`
	Content   any         `json:"content"`
	Timestamp string      `json:"timestamp"`
	ToolName  string      `json:"tool_name"`
	Name      string      `json:"name"`
	Type      string      `json:"type"`
	Message   *rawMessage `json:"message"`
}

type rawMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

var (
	directiveRE    = regexp.MustCompile(`(?i)\b(use|skip|avoid|defer|prefer|do not|don't|go with|keep|switch to|change|rename|must|should)\b`)
	choiceRE       = regexp.MustCompile(`(?i)\b(we decided|decided|chosen|choose|settled on|go with|use .{0,48} for now)\b`)
	rejectedRE     = regexp.MustCompile(`(?i)\b(rejected|do not use|don't use|rolled back|not using|tried .{0,80} but|avoid)\b`)
	handoffRE      = regexp.MustCompile(`(?i)\b(handoff|next step|next active task|blocker|unresolved|follow[- ]?up)\b`)
	completionRE   = regexp.MustCompile(`(?i)\b(done|completed|implemented|updated|changed|added|fixed|tests? passed|make test passed)\b`)
	personalFactRE = regexp.MustCompile(`(?i)\b(i (?:am|was|have|had|got|bought|visited|attended|graduated|work|prefer|like|love|enjoy|remember|recently)|my (?:doctor|physician|dermatologist|ent|sibling|brother|sister|family|role|job|setup|preference)|dr\.?\s+[A-Z][a-z]+|prescribed|appointment|biopsy|diagnosed|degree|university|college)\b`)
	pathishRE      = regexp.MustCompile(`\b([A-Za-z0-9_./-]+\.(go|ts|tsx|js|jsx|py|md|json|yaml|yml|toml|rs|swift|kt|java|rb|php|css|html))\b`)
)

func Ingest(data []byte, opts Options) (IngestResult, error) {
	opts = normalizeOptions(opts)
	turns, sessionID, err := ParseTurns(data)
	if err != nil {
		return IngestResult{}, err
	}
	if opts.SessionID == "" {
		opts.SessionID = sessionID
	}
	if opts.SessionID == "" {
		opts.SessionID = "sess_" + hashString(data)[:12]
	}
	records := Select(turns, opts)
	result := IngestResult{
		SessionID: opts.SessionID,
		Selected:  len(records),
		Skipped:   len(turns) - len(records),
	}
	for _, record := range records {
		result.Reasons = append(result.Reasons, record.SelectorReason)
	}
	if len(records) == 0 {
		return result, nil
	}
	path, sourcePath, err := writeRecords(records, opts)
	if err != nil {
		return IngestResult{}, err
	}
	result.Path = path
	result.SourcePath = sourcePath
	return result, nil
}

func ParseTurns(data []byte) ([]Turn, string, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, "", fmt.Errorf("transcript input is empty")
	}
	var transcript rawTranscript
	if data[0] == '{' {
		if err := json.Unmarshal(data, &transcript); err == nil {
			raw := firstRawTurns(transcript)
			if len(raw) > 0 {
				return normalizeTurns(raw), firstNonEmpty(transcript.SessionID, transcript.ID), nil
			}
		}
	}
	if data[0] == '[' {
		var raw []rawTurn
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, "", err
		}
		return normalizeTurns(raw), "", nil
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var raw []rawTurn
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var turn rawTurn
		if err := json.Unmarshal(line, &turn); err != nil {
			return nil, "", err
		}
		raw = append(raw, turn)
	}
	if err := scanner.Err(); err != nil {
		return nil, "", err
	}
	return normalizeTurns(raw), "", nil
}

func Select(turns []Turn, opts Options) []Record {
	opts = normalizeOptions(opts)
	var records []Record
	spanIndexes := map[string]int{}
	for i, turn := range turns {
		role := strings.ToLower(strings.TrimSpace(turn.Role))
		switch role {
		case "user":
			evidenceType, reason := classifyUser(turn.Content)
			if evidenceType == "" {
				continue
			}
			start, end, content := userWindow(turns, i)
			if !meetsFloor(evidenceType, content, opts.MinChars) {
				continue
			}
			records = addOrMergeRecord(records, spanIndexes, buildRecord(turns, opts, start, end, evidenceType, reason, content))
		case "assistant":
			evidenceType, reason := classifyAssistant(turns, i)
			if evidenceType == "" {
				continue
			}
			start, end, content := assistantWindow(turns, i)
			if !meetsFloor(evidenceType, content, opts.MinChars) {
				continue
			}
			records = addOrMergeRecord(records, spanIndexes, buildRecord(turns, opts, start, end, evidenceType, reason, content))
		}
	}
	return records
}

func List(stateDir, scopeID string) ([]File, error) {
	root, err := evidenceScopeDir(stateDir, scopeID)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var files []File
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		file, err := ReadFile(filepath.Join(root, entry.Name()), stateDir)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path > files[j].Path })
	return files, nil
}

func ReadFile(path, stateDir string) (File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return File{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return File{}, err
	}
	var records []Record
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var record Record
		if err := json.Unmarshal(line, &record); err != nil {
			return File{}, err
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return File{}, err
	}
	hash := sha256.Sum256(data)
	sessionID := ""
	if len(records) > 0 {
		sessionID = records[0].SessionID
	}
	return File{
		Path:       path,
		SourcePath: sourcePathForFile(path, stateDir),
		SessionID:  sessionID,
		Records:    records,
		Hash:       hex.EncodeToString(hash[:]),
		Size:       info.Size(),
		ModTime:    info.ModTime().UTC().Format(time.RFC3339),
	}, nil
}

func FindBySession(stateDir, scopeID, sessionID string) ([]File, error) {
	files, err := List(stateDir, scopeID)
	if err != nil {
		return nil, err
	}
	var matches []File
	for _, file := range files {
		if file.SessionID == sessionID || strings.Contains(filepath.Base(file.Path), sanitize(sessionID)) {
			matches = append(matches, file)
		}
	}
	return matches, nil
}

func normalizeOptions(opts Options) Options {
	opts.ScopeKind = strings.TrimSpace(opts.ScopeKind)
	opts.ScopeID = strings.TrimSpace(opts.ScopeID)
	opts.SourceAgent = strings.TrimSpace(opts.SourceAgent)
	opts.SessionID = strings.TrimSpace(opts.SessionID)
	if opts.MinChars <= 0 {
		opts.MinChars = DefaultMinChars
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now().UTC()
	} else {
		opts.Now = opts.Now.UTC()
	}
	return opts
}

func firstRawTurns(transcript rawTranscript) []rawTurn {
	switch {
	case len(transcript.Turns) > 0:
		return transcript.Turns
	case len(transcript.Messages) > 0:
		return transcript.Messages
	case len(transcript.Items) > 0:
		return transcript.Items
	default:
		return transcript.Events
	}
}

func normalizeTurns(raw []rawTurn) []Turn {
	turns := make([]Turn, 0, len(raw))
	for i, item := range raw {
		role := strings.ToLower(strings.TrimSpace(item.Role))
		contentValue := item.Content
		if item.Message != nil {
			if role == "" {
				role = strings.ToLower(strings.TrimSpace(item.Message.Role))
			}
			if contentValue == nil {
				contentValue = item.Message.Content
			}
		}
		if role == "" {
			role = strings.ToLower(strings.TrimSpace(item.Type))
		}
		content := strings.TrimSpace(contentString(contentValue))
		if content == "" {
			continue
		}
		index := item.TurnIndex
		if index == 0 {
			index = item.Index
		}
		if index == 0 {
			index = i + 1
		}
		toolName := firstNonEmpty(item.ToolName, item.Name)
		if role == "tool" && toolName == "" {
			toolName = strings.TrimSpace(item.Type)
		}
		turns = append(turns, Turn{
			Index:     index,
			Role:      role,
			Content:   content,
			Timestamp: strings.TrimSpace(item.Timestamp),
			ToolName:  toolName,
		})
	}
	return turns
}

func classifyUser(content string) (string, string) {
	switch {
	case rejectedRE.MatchString(content):
		return "rejected_path", "matched rejected-path language"
	case choiceRE.MatchString(content):
		return "explicit_choice", "matched explicit-choice language"
	case directiveRE.MatchString(content):
		return "user_directive", "matched user directive language"
	case personalFactRE.MatchString(content):
		return "personal_fact", "matched durable personal fact language"
	default:
		return "", ""
	}
}

func classifyAssistant(turns []Turn, i int) (string, string) {
	content := turns[i].Content
	if handoffRE.MatchString(content) {
		return "handoff_summary", "matched handoff or blocker language"
	}
	if completionRE.MatchString(content) && (hasNearbyTool(turns, i) || pathishRE.MatchString(content)) {
		return "completion_summary", "matched completion language with tool or path evidence"
	}
	return "", ""
}

func userWindow(turns []Turn, i int) (int, int, string) {
	start := i
	end := i
	for j := i + 1; j < len(turns); j++ {
		role := strings.ToLower(turns[j].Role)
		if role == "tool" {
			continue
		}
		if role == "assistant" {
			end = j
		}
		break
	}
	return turns[start].Index, turns[end].Index, formatWindow(turns[start : end+1])
}

func assistantWindow(turns []Turn, i int) (int, int, string) {
	start := i
	for j := i - 1; j >= 0 && j >= i-3; j-- {
		if strings.ToLower(turns[j].Role) == "user" {
			start = j
			break
		}
	}
	return turns[start].Index, turns[i].Index, formatWindow(turns[start : i+1])
}

func formatWindow(turns []Turn) string {
	var b strings.Builder
	for _, turn := range turns {
		role := strings.ToLower(strings.TrimSpace(turn.Role))
		if role == "tool" {
			continue
		}
		content := truncate(strings.TrimSpace(turn.Content), 1000)
		if content == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "%s: %s", role, oneLine(content))
	}
	return truncate(b.String(), maxContentChars)
}

func buildRecord(turns []Turn, opts Options, start, end int, evidenceType, reason, content string) Record {
	timestamp := ""
	for _, turn := range turns {
		if turn.Index == start && turn.Timestamp != "" {
			timestamp = turn.Timestamp
			break
		}
	}
	if timestamp == "" {
		timestamp = opts.Now.Format(time.RFC3339)
	}
	return Record{
		Version:        1,
		SessionID:      opts.SessionID,
		SourceAgent:    opts.SourceAgent,
		ScopeKind:      opts.ScopeKind,
		ScopeID:        opts.ScopeID,
		TurnStart:      start,
		TurnEnd:        end,
		EvidenceType:   evidenceType,
		EvidenceTypes:  []string{evidenceType},
		SelectorReason: reason,
		Timestamp:      timestamp,
		Redacted:       true,
		Content:        redact.Content(content),
	}
}

func addOrMergeRecord(records []Record, spanIndexes map[string]int, next Record) []Record {
	key := fmt.Sprintf("%d:%d", next.TurnStart, next.TurnEnd)
	if idx, ok := spanIndexes[key]; ok {
		records[idx].EvidenceTypes = appendUniqueString(records[idx].EvidenceTypes, next.EvidenceType)
		if !strings.Contains(records[idx].SelectorReason, next.SelectorReason) {
			records[idx].SelectorReason = records[idx].SelectorReason + "; " + next.SelectorReason
		}
		return records
	}
	spanIndexes[key] = len(records)
	return append(records, next)
}

func appendUniqueString(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func meetsFloor(evidenceType, content string, minChars int) bool {
	content = strings.TrimSpace(content)
	if evidenceType == "user_directive" || evidenceType == "explicit_choice" || evidenceType == "rejected_path" || evidenceType == "personal_fact" {
		return len(content) >= minDirectiveSize
	}
	return len(content) >= minChars
}

func hasNearbyTool(turns []Turn, i int) bool {
	for j := i - 1; j >= 0 && j >= i-8; j-- {
		if strings.ToLower(turns[j].Role) == "tool" || strings.TrimSpace(turns[j].ToolName) != "" {
			return true
		}
	}
	return false
}

func writeRecords(records []Record, opts Options) (string, string, error) {
	dir, err := evidenceScopeDir(opts.StateDir, opts.ScopeID)
	if err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", err
	}
	name := fmt.Sprintf("%s-%s.jsonl", sessionFilePrefix(opts.SessionID), opts.Now.Format("20060102T150405Z"))
	path := filepath.Join(dir, name)
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	for _, record := range records {
		if err := enc.Encode(record); err != nil {
			return "", "", err
		}
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		return "", "", err
	}
	return path, sourcePathForFile(path, opts.StateDir), nil
}

func sessionFilePrefix(sessionID string) string {
	clean := sanitize(sessionID)
	if strings.HasPrefix(clean, "sess_") || strings.HasPrefix(clean, "sess-") || strings.HasPrefix(clean, "sess.") || clean == "sess" {
		return clean
	}
	return "sess_" + clean
}

func evidenceScopeDir(stateDir, scopeID string) (string, error) {
	if strings.TrimSpace(stateDir) == "" {
		return "", fmt.Errorf("state dir is required")
	}
	if strings.TrimSpace(scopeID) == "" {
		return "", fmt.Errorf("scope ID is required")
	}
	return filepath.Join(stateDir, "session-evidence", sanitize(scopeID)), nil
}

func sourcePathForFile(path, stateDir string) string {
	rel, err := filepath.Rel(stateDir, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

func contentString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case []any:
		var parts []string
		for _, item := range v {
			if text := contentString(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	case map[string]any:
		if t, ok := v["type"].(string); ok {
			switch strings.ToLower(strings.TrimSpace(t)) {
			case "thinking", "tool_use", "tool_result", "image", "redacted_thinking":
				return ""
			}
		}
		for _, key := range []string{"text", "content", "message"} {
			if text := contentString(v[key]); text != "" {
				return text
			}
		}
		return ""
	default:
		data, _ := json.Marshal(v)
		return string(data)
	}
}

func oneLine(s string) string {
	fields := strings.Fields(strings.ReplaceAll(s, "\x00", " "))
	return strings.Join(fields, " ")
}

func truncate(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	if max <= 1 {
		return ""
	}
	out := s[:max-1]
	for !utf8.ValidString(out) && len(out) > 0 {
		out = out[:len(out)-1]
	}
	return strings.TrimSpace(out) + "..."
}

func sanitize(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "._-")
	if out == "" {
		return "unknown"
	}
	if len(out) > 80 {
		return out[:80]
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func hashString(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
