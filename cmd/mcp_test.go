package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/1broseidon/recoil/internal/scope"
	"github.com/1broseidon/recoil/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPToolsListReadOnly(t *testing.T) {
	session, cleanup := connectMCPTestServer(t, mcpOptions{})
	defer cleanup()

	result, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	names := mcpToolNames(result.Tools)
	if !names["recoil_search"] || !names["recoil_wake"] || !names["recoil_check"] {
		t.Fatalf("expected read tools, got %+v", names)
	}
	if names["recoil_add"] || names["recoil_remember"] || names["recoil_handoff"] {
		t.Fatalf("did not expect write tool without --allow-write, got %+v", names)
	}
}

func TestMCPAllowWriteListsAddTool(t *testing.T) {
	session, cleanup := connectMCPTestServer(t, mcpOptions{allowWrite: true})
	defer cleanup()

	result, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	names := mcpToolNames(result.Tools)
	for _, name := range []string{"recoil_add", "recoil_remember", "recoil_handoff"} {
		if !names[name] {
			t.Fatalf("expected %s in tools: %+v", name, names)
		}
	}
}

func TestMCPSearchMatchesCLITopResultAndEmptyMessage(t *testing.T) {
	activeID := setupMCPParityProject(t)
	session, cleanup := connectMCPTestServer(t, mcpOptions{})
	defer cleanup()

	query := "mcp parity active retrieval"
	mcpSearch, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "recoil_search",
		Arguments: map[string]any{"query": query, "limit": 5},
	})
	if err != nil {
		t.Fatal(err)
	}
	mcpText := mcpToolText(t, mcpSearch)
	if got := mcpToolEnvelopeKind(t, mcpSearch); got != "search_result" {
		t.Fatalf("expected structured search_result, got %q", got)
	}

	searchCmd := newSearchCommand()
	var cliOut bytes.Buffer
	searchCmd.SetOut(&cliOut)
	searchCmd.SetErr(&bytes.Buffer{})
	searchCmd.SetArgs([]string{"--limit", "5", query})
	if err := searchCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	cliBody := stripFrontmatter(cliOut.String())

	if got, want := firstMemoryID(mcpText), firstMemoryID(cliBody); got != want || got != activeID {
		t.Fatalf("MCP/CLI top result mismatch: mcp=%q cli=%q active=%q\nMCP:\n%s\nCLI:\n%s", got, want, activeID, mcpText, cliBody)
	}

	emptyQuery := "zzzxxy qqqnohit"
	mcpEmpty, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "recoil_search",
		Arguments: map[string]any{"query": emptyQuery, "limit": 5},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := mcpToolEnvelopeKind(t, mcpEmpty); got != "search_result" {
		t.Fatalf("expected structured empty search_result, got %q", got)
	}
	emptyCmd := newSearchCommand()
	var emptyCLI bytes.Buffer
	emptyCmd.SetOut(&emptyCLI)
	emptyCmd.SetErr(&bytes.Buffer{})
	emptyCmd.SetArgs([]string{"--limit", "5", emptyQuery})
	if err := emptyCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(mcpToolText(t, mcpEmpty)), strings.TrimSpace(stripFrontmatter(emptyCLI.String())); got != want {
		t.Fatalf("MCP/CLI empty search mismatch: got %q want %q", got, want)
	}
}

func TestMCPWakeMatchesCLIBody(t *testing.T) {
	setupMCPParityProject(t)
	session, cleanup := connectMCPTestServer(t, mcpOptions{})
	defer cleanup()

	query := "mcp parity active retrieval"
	mcpWake, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "recoil_wake",
		Arguments: map[string]any{"query": query, "limit": 4, "max_chars": 2000},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := mcpToolEnvelopeKind(t, mcpWake); got != "wake_result" {
		t.Fatalf("expected structured wake_result, got %q", got)
	}

	wakeCmd := newWakeCommand()
	var cliOut bytes.Buffer
	wakeCmd.SetOut(&cliOut)
	wakeCmd.SetErr(&bytes.Buffer{})
	wakeCmd.SetArgs([]string{"--limit", "4", "--max-chars", "2000", query})
	if err := wakeCmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if got, want := strings.TrimSpace(mcpToolText(t, mcpWake)), strings.TrimSpace(stripFrontmatter(cliOut.String())); got != want {
		t.Fatalf("MCP/CLI wake body mismatch\nMCP:\n%s\nCLI:\n%s", got, want)
	}
}

func TestMCPToolErrorsAreStructured(t *testing.T) {
	setupMCPParityProject(t)
	session, cleanup := connectMCPTestServer(t, mcpOptions{})
	defer cleanup()

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "recoil_search",
		Arguments: map[string]any{"query": ""},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatalf("expected tool error")
	}
	if got := mcpToolEnvelopeKind(t, result); got != "error" {
		t.Fatalf("expected structured error envelope, got %q", got)
	}
}

func TestMCPWorkflowToolsRememberCheckHandoff(t *testing.T) {
	setupMCPParityProject(t)
	session, cleanup := connectMCPTestServer(t, mcpOptions{allowWrite: true})
	defer cleanup()

	remembered, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "recoil_remember",
		Arguments: map[string]any{
			"content":   "Decision: use structured MCP workflow tools for agent memory.",
			"agent":     "codex",
			"claim_key": "mcp.workflow",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := mcpToolEnvelopeKind(t, remembered); got != "remember_result" {
		t.Fatalf("expected structured remember_result, got %q", got)
	}

	checked, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "recoil_check",
		Arguments: map[string]any{"claim_key": "mcp.workflow", "limit": 5},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := mcpToolEnvelopeKind(t, checked); got != "check_result" {
		t.Fatalf("expected structured check_result, got %q", got)
	}
	if !strings.Contains(mcpToolText(t, checked), "mcp.workflow") {
		t.Fatalf("expected check text to mention claim key, got:\n%s", mcpToolText(t, checked))
	}

	handoff, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "recoil_handoff",
		Arguments: map[string]any{
			"summary":    "MCP workflow tools were exercised.",
			"agent":      "codex",
			"next_steps": []string{"Keep MCP and CLI contracts aligned."},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := mcpToolEnvelopeKind(t, handoff); got != "handoff_result" {
		t.Fatalf("expected structured handoff_result, got %q", got)
	}
}

func connectMCPTestServer(t *testing.T, opts mcpOptions) (*mcp.ClientSession, func()) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := newRecoilMCPServer(opts).Connect(ctx, serverTransport, nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "recoil-test-client", Version: "test"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		_ = serverSession.Close()
		cancel()
		t.Fatal(err)
	}
	return clientSession, func() {
		_ = clientSession.Close()
		_ = serverSession.Close()
		cancel()
	}
}

func mcpToolNames(tools []*mcp.Tool) map[string]bool {
	names := make(map[string]bool, len(tools))
	for _, tool := range tools {
		names[tool.Name] = true
	}
	return names
}

func setupMCPParityProject(t *testing.T) string {
	t.Helper()
	oldOpts := opts
	opts = globalOptions{dbPath: filepath.Join(t.TempDir(), "recoil.db")}
	t.Cleanup(func() { opts = oldOpts })

	root := t.TempDir()
	if _, err := scope.InitProject(root); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	sc, err := scope.ProjectScope(".")
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(opts.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	if _, _, err := st.AddMemory(ctx, store.AddMemoryParams{
		Role:      "decision",
		Content:   "MCP parity rejected retrieval memory should stay historical.",
		ScopeKind: sc.Kind,
		ScopeID:   sc.ID,
		ProjectID: sc.ProjectID,
		Validity:  "rejected",
		ClaimKey:  "mcp.parity",
	}); err != nil {
		t.Fatal(err)
	}
	active, _, err := st.AddMemory(ctx, store.AddMemoryParams{
		Role:      "decision",
		Content:   "MCP parity active retrieval memory is the current answer.",
		ScopeKind: sc.Kind,
		ScopeID:   sc.ID,
		ProjectID: sc.ProjectID,
		Validity:  "active",
		ClaimKey:  "mcp.parity",
	})
	if err != nil {
		t.Fatal(err)
	}
	return active.ID
}

func mcpToolText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if result.IsError {
		t.Fatalf("tool returned error: %+v", result.Content)
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected one content item, got %d", len(result.Content))
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected text content, got %T", result.Content[0])
	}
	return text.Text
}

func mcpToolEnvelopeKind(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if result.StructuredContent == nil {
		t.Fatalf("expected structured content")
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("structured content is not an envelope: %v\n%s", err, data)
	}
	return got.Kind
}

func stripFrontmatter(text string) string {
	if !strings.HasPrefix(text, "---\n") {
		return text
	}
	idx := strings.Index(text[4:], "\n---\n")
	if idx < 0 {
		return text
	}
	return text[idx+9:]
}

var memoryHeadingRE = regexp.MustCompile(`(?m)^## (mem_[a-z0-9]+)$`)

func firstMemoryID(text string) string {
	match := memoryHeadingRE.FindStringSubmatch(text)
	if len(match) == 0 {
		return ""
	}
	return match[1]
}
