package cmd

import (
	"bytes"
	"context"
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
	if !names["recoil_search"] || !names["recoil_wake"] {
		t.Fatalf("expected read tools, got %+v", names)
	}
	if names["recoil_add"] {
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
	if !names["recoil_add"] {
		t.Fatalf("expected recoil_add in tools: %+v", names)
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
