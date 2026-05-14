package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/1broseidon/recoil/internal/scope"
	"github.com/1broseidon/recoil/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

type mcpOptions struct {
	allowWrite bool
}

type mcpSearchInput struct {
	Query string `json:"query" jsonschema:"Search query."`
	Limit int    `json:"limit,omitempty" jsonschema:"Maximum memories to return."`
}

type mcpWakeInput struct {
	Query    string `json:"query,omitempty" jsonschema:"Optional focus query."`
	Limit    int    `json:"limit,omitempty" jsonschema:"Maximum memories to include."`
	MaxChars int    `json:"max_chars,omitempty" jsonschema:"Maximum rendered characters."`
}

type mcpAddInput struct {
	Content  string `json:"content" jsonschema:"Memory content."`
	Role     string `json:"role,omitempty" jsonschema:"Optional memory role."`
	ClaimKey string `json:"claim_key,omitempty" jsonschema:"Optional stable claim key."`
	Validity string `json:"validity,omitempty" jsonschema:"Optional validity state."`
}

func newMCPCommand() *cobra.Command {
	var mcpOpts mcpOptions
	c := &cobra.Command{
		Use:   "mcp",
		Short: "Run a stdio MCP server for recoil memory tools",
		Long: `Run a Model Context Protocol stdio server exposing recoil memory tools
through the official MCP Go SDK.

The server is read-only by default. Start with --allow-write to expose the
recoil_add tool.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMCPServer(context.Background(), &mcp.StdioTransport{}, mcpOpts)
		},
	}
	c.Flags().BoolVar(&mcpOpts.allowWrite, "allow-write", false, "expose write-capable MCP tools")
	return c
}

func runMCPServer(ctx context.Context, transport mcp.Transport, opts mcpOptions) error {
	return newRecoilMCPServer(opts).Run(ctx, transport)
}

func newRecoilMCPServer(opts mcpOptions) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "recoil", Version: version}, &mcp.ServerOptions{
		Instructions: "Use Recoil tools to search and wake bounded local project memory before guessing from prior chat context.",
	})
	mcp.AddTool(server, &mcp.Tool{
		Name:        "recoil_search",
		Description: "Search current Recoil memories in the active project scope.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input mcpSearchInput) (*mcp.CallToolResult, any, error) {
		result, err := mcpSearch(ctx, input)
		return result, nil, err
	})
	mcp.AddTool(server, &mcp.Tool{
		Name:        "recoil_wake",
		Description: "Return bounded startup memory context for the active project scope.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input mcpWakeInput) (*mcp.CallToolResult, any, error) {
		result, err := mcpWake(ctx, input)
		return result, nil, err
	})
	if opts.allowWrite {
		mcp.AddTool(server, &mcp.Tool{
			Name:        "recoil_add",
			Description: "Add a memory to the active project scope.",
		}, func(ctx context.Context, req *mcp.CallToolRequest, input mcpAddInput) (*mcp.CallToolResult, any, error) {
			result, err := mcpAdd(ctx, input)
			return result, nil, err
		})
	}
	return server
}

func mcpSearch(ctx context.Context, req mcpSearchInput) (*mcp.CallToolResult, error) {
	req.Query = strings.TrimSpace(req.Query)
	if req.Query == "" {
		return nil, fmt.Errorf("query is required")
	}
	if req.Limit <= 0 {
		req.Limit = 5
	}
	sc, err := scope.ProjectScope(".")
	if err != nil {
		return nil, err
	}
	st, _, err := openStore()
	if err != nil {
		return nil, err
	}
	defer st.Close()
	params := store.SearchParams{
		Query:        req.Query,
		ScopeKind:    sc.Kind,
		ScopeID:      sc.ID,
		Limit:        req.Limit,
		Lifecycle:    store.LifecycleCurrent,
		SignalRerank: true,
	}
	_, settings, err := loadProjectSettings()
	if err == nil {
		params.SourceQuality = effectiveSourceQualityOptions(settings)
	}
	rows, err := runRetriever(ctx, st, params, retrieverOptions{
		mode:  retrievalFTS,
		limit: req.Limit,
	})
	if err != nil {
		return nil, err
	}
	rows, err = augmentProfileSearch(ctx, st, params, rows, "auto")
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return mcpTextResult("No current memories found."), nil
	}
	return mcpTextResult(memoryBlocks(rows, 4000, true)), nil
}

func mcpWake(ctx context.Context, req mcpWakeInput) (*mcp.CallToolResult, error) {
	if req.Limit <= 0 {
		req.Limit = 8
	}
	if req.MaxChars <= 0 {
		req.MaxChars = 1600
	}
	sc, err := scope.ProjectScope(".")
	if err != nil {
		return nil, err
	}
	st, _, err := openStore()
	if err != nil {
		return nil, err
	}
	defer st.Close()
	var queryResults []store.Memory
	_, settings, _ := loadProjectSettings()
	quality := effectiveSourceQualityOptions(settings)
	if strings.TrimSpace(req.Query) != "" {
		fetchLimit := wakeFetchLimit(req.Limit)
		found, err := runRetriever(ctx, st, store.SearchParams{
			Query:         req.Query,
			ScopeKind:     sc.Kind,
			ScopeID:       sc.ID,
			Limit:         fetchLimit,
			Lifecycle:     store.LifecycleCurrent,
			SignalRerank:  true,
			SourceQuality: quality,
		}, retrieverOptions{
			mode:  retrievalFTS,
			limit: fetchLimit,
		})
		if err != nil {
			return nil, err
		}
		queryResults = found
	}
	recent, err := wakeRecentMemories(ctx, st, sc, memoryFilterOptions{}, wakeFetchLimit(req.Limit), req.Limit, quality)
	if err != nil {
		return nil, err
	}
	layers := buildWakeLayers(req.Query, queryResults, recent, req.Limit, quality)
	rendered := layeredMemoryBlocks(layers, req.MaxChars, true)
	return mcpTextResult(rendered.Body), nil
}

func mcpAdd(ctx context.Context, req mcpAddInput) (*mcp.CallToolResult, error) {
	req.Content = strings.TrimSpace(req.Content)
	if req.Content == "" {
		return nil, fmt.Errorf("content is required")
	}
	sc, err := scope.ProjectScope(".")
	if err != nil {
		return nil, err
	}
	st, _, err := openStore()
	if err != nil {
		return nil, err
	}
	defer st.Close()
	mem, duplicate, err := st.AddMemory(ctx, store.AddMemoryParams{
		Role:       req.Role,
		Content:    req.Content,
		SourceKind: "direct",
		ScopeKind:  sc.Kind,
		ScopeID:    sc.ID,
		ProjectID:  sc.ProjectID,
		Validity:   req.Validity,
		ClaimKey:   req.ClaimKey,
	})
	if err != nil {
		return nil, err
	}
	data, _ := json.MarshalIndent(map[string]any{"memory": mem, "duplicate": duplicate}, "", "  ")
	return mcpTextResult(string(data)), nil
}

func mcpTextResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}
}
