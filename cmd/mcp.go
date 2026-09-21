package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/1broseidon/recoil/internal/embedding"
	"github.com/1broseidon/recoil/internal/scope"
	"github.com/1broseidon/recoil/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

type mcpOptions struct {
	allowWrite bool
}

type mcpSearchInput struct {
	Query          string `json:"query" jsonschema:"Search query."`
	Limit          int    `json:"limit,omitempty" jsonschema:"Maximum memories to return."`
	MaxChars       int    `json:"max_chars,omitempty" jsonschema:"Maximum rendered characters for text content."`
	User           bool   `json:"user,omitempty" jsonschema:"Use persistent user scope."`
	Project        string `json:"project,omitempty" jsonschema:"Use project scope for the given workspace path."`
	Session        string `json:"session,omitempty" jsonschema:"Use session scope for the given session ID."`
	Since          string `json:"since,omitempty" jsonschema:"Filter memories created since a date or duration like 7d."`
	Before         string `json:"before,omitempty" jsonschema:"Filter memories created before a date or duration like 7d."`
	SourceKind     string `json:"source_kind,omitempty" jsonschema:"Filter by source kind."`
	Agent          string `json:"agent,omitempty" jsonschema:"Filter by source agent."`
	Source         string `json:"source,omitempty" jsonschema:"Filter by source path substring."`
	Role           string `json:"role,omitempty" jsonschema:"Filter by exact role."`
	ClaimKey       string `json:"claim_key,omitempty" jsonschema:"Filter by exact claim key."`
	Validity       string `json:"validity,omitempty" jsonschema:"Filter by exact validity state."`
	Current        bool   `json:"current,omitempty" jsonschema:"Filter to current memories."`
	Historical     bool   `json:"historical,omitempty" jsonschema:"Filter to historical, rejected, superseded, stale, or tombstoned memories."`
	Hybrid         bool   `json:"hybrid,omitempty" jsonschema:"Fuse FTS5 and embedding similarity via RRF."`
	HybridProvider string `json:"hybrid_provider,omitempty" jsonschema:"Embedding provider for hybrid search."`
	HybridModel    string `json:"hybrid_model,omitempty" jsonschema:"Embedding model for hybrid search."`
	HybridPool     int    `json:"hybrid_pool,omitempty" jsonschema:"Candidate pool size per retrieval method before fusion."`
	FusionK        int    `json:"fusion_k,omitempty" jsonschema:"RRF fusion constant."`
	Profiles       string `json:"profiles,omitempty" jsonschema:"Profile retrieval mode: auto, on, or off."`
}

type mcpWakeInput struct {
	Query            string `json:"query,omitempty" jsonschema:"Optional focus query."`
	Limit            int    `json:"limit,omitempty" jsonschema:"Maximum memories to include."`
	MaxChars         int    `json:"max_chars,omitempty" jsonschema:"Maximum rendered characters."`
	IncludeDecisions bool   `json:"include_decisions,omitempty" jsonschema:"Include a claim-keyed decision trail."`
	User             bool   `json:"user,omitempty" jsonschema:"Use persistent user scope."`
	Project          string `json:"project,omitempty" jsonschema:"Use project scope for the given workspace path."`
	Session          string `json:"session,omitempty" jsonschema:"Use session scope for the given session ID."`
	Since            string `json:"since,omitempty" jsonschema:"Filter memories created since a date or duration like 7d."`
	Before           string `json:"before,omitempty" jsonschema:"Filter memories created before a date or duration like 7d."`
	SourceKind       string `json:"source_kind,omitempty" jsonschema:"Filter by source kind."`
	Agent            string `json:"agent,omitempty" jsonschema:"Filter by source agent."`
	Source           string `json:"source,omitempty" jsonschema:"Filter by source path substring."`
	Role             string `json:"role,omitempty" jsonschema:"Filter by exact role."`
	ClaimKey         string `json:"claim_key,omitempty" jsonschema:"Filter by exact claim key."`
	Validity         string `json:"validity,omitempty" jsonschema:"Filter by exact validity state."`
	Current          bool   `json:"current,omitempty" jsonschema:"Filter to current memories."`
	Historical       bool   `json:"historical,omitempty" jsonschema:"Filter to historical, rejected, superseded, stale, or tombstoned memories."`
}

type mcpAddInput struct {
	Content      string `json:"content" jsonschema:"Memory content."`
	Role         string `json:"role,omitempty" jsonschema:"Optional memory role."`
	SourceKind   string `json:"source_kind,omitempty" jsonschema:"Source kind: direct, file, session_evidence, extracted_claim."`
	Agent        string `json:"agent,omitempty" jsonschema:"Source agent name."`
	SourcePath   string `json:"source_path,omitempty" jsonschema:"Source file or transcript path."`
	SourceRef    string `json:"source_ref,omitempty" jsonschema:"Source reference within the path."`
	Metadata     string `json:"metadata,omitempty" jsonschema:"Custom metadata as JSON."`
	Validity     string `json:"validity,omitempty" jsonschema:"Validity state."`
	ClaimKey     string `json:"claim_key,omitempty" jsonschema:"Optional stable claim key."`
	Supersedes   string `json:"supersedes,omitempty" jsonschema:"Memory ID this memory supersedes."`
	SupersededBy string `json:"superseded_by,omitempty" jsonschema:"Memory ID that supersedes this memory."`
	User         bool   `json:"user,omitempty" jsonschema:"Use persistent user scope."`
	Project      string `json:"project,omitempty" jsonschema:"Use project scope for the given workspace path."`
	Session      string `json:"session,omitempty" jsonschema:"Use session scope for the given session ID."`
}

type mcpRememberInput struct {
	Content      string `json:"content" jsonschema:"Memory content."`
	Role         string `json:"role,omitempty" jsonschema:"Override inferred role."`
	Agent        string `json:"agent,omitempty" jsonschema:"Source agent name."`
	SourcePath   string `json:"source_path,omitempty" jsonschema:"Source file or transcript path."`
	SourceRef    string `json:"source_ref,omitempty" jsonschema:"Source reference within the path."`
	Metadata     string `json:"metadata,omitempty" jsonschema:"Custom metadata as JSON."`
	Validity     string `json:"validity,omitempty" jsonschema:"Validity state."`
	ClaimKey     string `json:"claim_key,omitempty" jsonschema:"Override inferred claim key."`
	Supersedes   string `json:"supersedes,omitempty" jsonschema:"Memory ID this memory supersedes."`
	SupersededBy string `json:"superseded_by,omitempty" jsonschema:"Memory ID that supersedes this memory."`
	User         bool   `json:"user,omitempty" jsonschema:"Use persistent user scope."`
	Project      string `json:"project,omitempty" jsonschema:"Use project scope for the given workspace path."`
	Session      string `json:"session,omitempty" jsonschema:"Use session scope for the given session ID."`
}

type mcpHandoffInput struct {
	Summary       string   `json:"summary,omitempty" jsonschema:"Handoff summary."`
	Agent         string   `json:"agent,omitempty" jsonschema:"Source agent name."`
	Decisions     []string `json:"decisions,omitempty" jsonschema:"Decisions made this session."`
	Constraints   []string `json:"constraints,omitempty" jsonschema:"Constraints discovered this session."`
	NextSteps     []string `json:"next_steps,omitempty" jsonschema:"Next steps for the next session."`
	OpenQuestions []string `json:"open_questions,omitempty" jsonschema:"Open questions for the next session."`
	Supersedes    string   `json:"supersedes,omitempty" jsonschema:"Previous handoff memory this handoff supersedes."`
	ClaimKey      string   `json:"claim_key,omitempty" jsonschema:"Claim key for the handoff memory."`
	User          bool     `json:"user,omitempty" jsonschema:"Use persistent user scope."`
	Project       string   `json:"project,omitempty" jsonschema:"Use project scope for the given workspace path."`
	Session       string   `json:"session,omitempty" jsonschema:"Use session scope for the given session ID."`
}

type mcpCheckInput struct {
	Query          string `json:"query,omitempty" jsonschema:"Decision query or memory ID."`
	ClaimKey       string `json:"claim_key,omitempty" jsonschema:"Exact claim family to audit."`
	ClaimKeyPrefix string `json:"claim_key_prefix,omitempty" jsonschema:"Audit every claim family under this prefix. Cannot be combined with query or claim_key."`
	Limit          int    `json:"limit,omitempty" jsonschema:"Maximum memories to inspect."`
	User           bool   `json:"user,omitempty" jsonschema:"Use persistent user scope."`
	Project        string `json:"project,omitempty" jsonschema:"Use project scope for the given workspace path."`
	Session        string `json:"session,omitempty" jsonschema:"Use session scope for the given session ID."`
}

type mcpListInput struct {
	Limit          int    `json:"limit,omitempty" jsonschema:"Maximum memories to list."`
	MaxChars       int    `json:"max_chars,omitempty" jsonschema:"Maximum rendered characters for text content."`
	User           bool   `json:"user,omitempty" jsonschema:"Use persistent user scope."`
	Project        string `json:"project,omitempty" jsonschema:"Use project scope for the given workspace path."`
	Session        string `json:"session,omitempty" jsonschema:"Use session scope for the given session ID."`
	Since          string `json:"since,omitempty" jsonschema:"Filter memories created since a date or duration like 7d."`
	Before         string `json:"before,omitempty" jsonschema:"Filter memories created before a date or duration like 7d."`
	SourceKind     string `json:"source_kind,omitempty" jsonschema:"Filter by source kind."`
	Agent          string `json:"agent,omitempty" jsonschema:"Filter by source agent."`
	Source         string `json:"source,omitempty" jsonschema:"Filter by source path substring."`
	Role           string `json:"role,omitempty" jsonschema:"Filter by exact role."`
	ClaimKey       string `json:"claim_key,omitempty" jsonschema:"Filter by exact claim key."`
	ClaimKeyPrefix string `json:"claim_key_prefix,omitempty" jsonschema:"Filter by claim key family prefix. Cannot be combined with claim_key."`
	Validity       string `json:"validity,omitempty" jsonschema:"Filter by exact validity state."`
	Current        bool   `json:"current,omitempty" jsonschema:"Filter to current memories."`
	Historical     bool   `json:"historical,omitempty" jsonschema:"Filter to historical, rejected, superseded, stale, or tombstoned memories."`
	IncludeDeleted bool   `json:"include_deleted,omitempty" jsonschema:"Include tombstoned memories."`
}

type mcpExportInput struct {
	ClaimKey       string `json:"claim_key,omitempty" jsonschema:"Export one exact claim family."`
	ClaimKeyPrefix string `json:"claim_key_prefix,omitempty" jsonschema:"Export every claim family under this prefix. One of claim_key or claim_key_prefix is required."`
	Historical     bool   `json:"historical,omitempty" jsonschema:"Export past memories instead of only current ones."`
	AllValidity    bool   `json:"all_validity,omitempty" jsonschema:"Export current and past memories together."`
	Limit          int    `json:"limit,omitempty" jsonschema:"Maximum memories to export. 0 means every match."`
	User           bool   `json:"user,omitempty" jsonschema:"Use persistent user scope."`
	Project        string `json:"project,omitempty" jsonschema:"Use project scope for the given workspace path."`
	Session        string `json:"session,omitempty" jsonschema:"Use session scope for the given session ID."`
}

type mcpClaimsInput struct {
	User           bool   `json:"user,omitempty" jsonschema:"Use persistent user scope."`
	Project        string `json:"project,omitempty" jsonschema:"Use project scope for the given workspace path."`
	Session        string `json:"session,omitempty" jsonschema:"Use session scope for the given session ID."`
	ClaimKey       string `json:"claim_key,omitempty" jsonschema:"Filter to one exact claim key."`
	ClaimKeyPrefix string `json:"claim_key_prefix,omitempty" jsonschema:"Filter the family index to this prefix. Cannot be combined with claim_key."`
}

func newMCPCommand() *cobra.Command {
	var mcpOpts mcpOptions
	c := &cobra.Command{
		Use:   "mcp",
		Short: "Run a stdio MCP server for recoil memory tools",
		Long: `Run a Model Context Protocol stdio server exposing recoil memory tools
through the official MCP Go SDK.

The server exposes read-only search, wake, and check tools by default. Start
with --allow-write to expose write-capable add, remember, and handoff tools.`,
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
		Instructions: "Use Recoil tools to search, wake, and check bounded local project memory before guessing from prior chat context.",
	})
	mcp.AddTool(server, &mcp.Tool{
		Name:        "recoil_search",
		Description: "Search Recoil memories with the same freshness and retrieval lanes as the CLI.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input mcpSearchInput) (*mcp.CallToolResult, envelope, error) {
		result, text, err := mcpSearch(ctx, input)
		return mcpToolResult("search_result", result, text, err)
	})
	mcp.AddTool(server, &mcp.Tool{
		Name:        "recoil_wake",
		Description: "Return bounded startup memory context with the same source freshness as the CLI.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input mcpWakeInput) (*mcp.CallToolResult, envelope, error) {
		result, text, err := mcpWake(ctx, input)
		return mcpToolResult("wake_result", result, text, err)
	})
	mcp.AddTool(server, &mcp.Tool{
		Name:        "recoil_check",
		Description: "Audit whether a remembered decision is still safe to act on.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input mcpCheckInput) (*mcp.CallToolResult, envelope, error) {
		result, kind, text, err := mcpCheck(ctx, input)
		return mcpToolResult(kind, result, text, err)
	})
	mcp.AddTool(server, &mcp.Tool{
		Name:        "recoil_list",
		Description: "List scoped memories in deterministic order with no ranking, no query, and no truncation of the JSON payload.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input mcpListInput) (*mcp.CallToolResult, envelope, error) {
		result, text, err := mcpList(ctx, input)
		return mcpToolResult("list_result", result, text, err)
	})
	mcp.AddTool(server, &mcp.Tool{
		Name:        "recoil_claims",
		Description: "List claim-key families in scope, optionally narrowed to one key or one family prefix.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input mcpClaimsInput) (*mcp.CallToolResult, envelope, error) {
		result, text, err := mcpClaims(ctx, input)
		return mcpToolResult("claims_result", result, text, err)
	})
	mcp.AddTool(server, &mcp.Tool{
		Name:        "recoil_export",
		Description: "Export whole claim families verbatim for doctrine injection: no ranking, no truncation, stable order.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input mcpExportInput) (*mcp.CallToolResult, envelope, error) {
		result, text, err := mcpExport(ctx, input)
		return mcpToolResult("export_result", result, text, err)
	})
	if opts.allowWrite {
		mcp.AddTool(server, &mcp.Tool{
			Name:        "recoil_add",
			Description: "Add a low-level memory to the active Recoil scope.",
		}, func(ctx context.Context, req *mcp.CallToolRequest, input mcpAddInput) (*mcp.CallToolResult, envelope, error) {
			result, text, err := mcpAdd(ctx, input)
			return mcpToolResult("add_result", result, text, err)
		})
		mcp.AddTool(server, &mcp.Tool{
			Name:        "recoil_remember",
			Description: "Remember useful project context with deterministic role inference.",
		}, func(ctx context.Context, req *mcp.CallToolRequest, input mcpRememberInput) (*mcp.CallToolResult, envelope, error) {
			result, text, err := mcpRemember(ctx, input)
			return mcpToolResult("remember_result", result, text, err)
		})
		mcp.AddTool(server, &mcp.Tool{
			Name:        "recoil_handoff",
			Description: "Close a session with structured next context for the next agent.",
		}, func(ctx context.Context, req *mcp.CallToolRequest, input mcpHandoffInput) (*mcp.CallToolResult, envelope, error) {
			result, text, err := mcpHandoff(ctx, input)
			return mcpToolResult("handoff_result", result, text, err)
		})
	}
	return server
}

func mcpSearch(ctx context.Context, req mcpSearchInput) (searchResult, string, error) {
	req.Query = strings.TrimSpace(req.Query)
	if req.Query == "" {
		return searchResult{}, "", fmt.Errorf("query is required")
	}
	if req.Limit <= 0 {
		req.Limit = 5
	}
	if req.MaxChars <= 0 {
		req.MaxChars = 4000
	}
	sc, err := resolveMCPScope(scopeOptions{user: req.User, project: req.Project, session: req.Session})
	if err != nil {
		return searchResult{}, "", err
	}
	st, _, err := openStore()
	if err != nil {
		return searchResult{}, "", err
	}
	defer st.Close()
	hybridProvider := req.HybridProvider
	hybridModel := req.HybridModel
	if req.Hybrid {
		hybridProvider = firstNonEmpty(hybridProvider, embedding.OpenRouterProvider)
		hybridModel = firstNonEmpty(hybridModel, embedding.DefaultOpenRouterModel)
	}
	result, err := runSearch(ctx, st, sc, req.Query, searchOptions{
		filters: memoryFilterOptions{
			since:      req.Since,
			before:     req.Before,
			sourceKind: req.SourceKind,
			agent:      req.Agent,
			source:     req.Source,
			role:       req.Role,
			claimKey:   req.ClaimKey,
			validity:   req.Validity,
			current:    req.Current,
			historical: req.Historical,
		},
		limit:          req.Limit,
		hybrid:         req.Hybrid,
		hybridProvider: hybridProvider,
		hybridModel:    hybridModel,
		hybridPool:     req.HybridPool,
		fusionK:        req.FusionK,
		profiles:       firstNonEmpty(req.Profiles, "auto"),
	})
	if err != nil {
		return searchResult{}, "", err
	}
	return result, renderSearchResult(result, req.MaxChars), nil
}

func mcpWake(ctx context.Context, req mcpWakeInput) (wakeResult, string, error) {
	if req.Limit <= 0 {
		req.Limit = 8
	}
	if req.MaxChars <= 0 {
		req.MaxChars = 1600
	}
	sc, err := resolveMCPScope(scopeOptions{user: req.User, project: req.Project, session: req.Session})
	if err != nil {
		return wakeResult{}, "", err
	}
	st, _, err := openStore()
	if err != nil {
		return wakeResult{}, "", err
	}
	defer st.Close()
	exec, err := runWake(ctx, st, sc, strings.TrimSpace(req.Query), wakeOptions{
		filters: memoryFilterOptions{
			since:      req.Since,
			before:     req.Before,
			sourceKind: req.SourceKind,
			agent:      req.Agent,
			source:     req.Source,
			role:       req.Role,
			claimKey:   req.ClaimKey,
			validity:   req.Validity,
			current:    req.Current,
			historical: req.Historical,
		},
		limit:     req.Limit,
		maxChars:  req.MaxChars,
		decisions: req.IncludeDecisions,
	})
	if err != nil {
		return wakeResult{}, "", err
	}
	text, _ := renderWakeExecution(exec, req.MaxChars)
	return exec.Result, text, nil
}

// mcpCheck mirrors the CLI: a claim_key_prefix audits every family under the
// prefix and returns the check_prefix_result envelope, so the returned kind
// varies with the input.
func mcpCheck(ctx context.Context, req mcpCheckInput) (any, string, string, error) {
	query := strings.TrimSpace(req.Query)
	claimKey := strings.TrimSpace(req.ClaimKey)
	claimKeyPrefix := strings.TrimSpace(req.ClaimKeyPrefix)
	if err := validateCheckTarget(query, claimKey, claimKeyPrefix); err != nil {
		return nil, "check_result", "", err
	}
	if req.Limit <= 0 {
		req.Limit = 8
	}
	sc, err := resolveMCPScope(scopeOptions{user: req.User, project: req.Project, session: req.Session})
	if err != nil {
		return nil, "check_result", "", err
	}
	st, _, err := openStore()
	if err != nil {
		return nil, "check_result", "", err
	}
	defer st.Close()
	if claimKeyPrefix != "" {
		result, err := runCheckPrefix(ctx, st, sc, claimKeyPrefix, req.Limit)
		if err != nil {
			return nil, "check_prefix_result", "", err
		}
		return result, "check_prefix_result", renderCheckPrefixResult(result), nil
	}
	result, err := runCheck(ctx, st, sc, query, claimKey, req.Limit)
	if err != nil {
		return nil, "check_result", "", err
	}
	return result, "check_result", renderCheckResult(result), nil
}

func mcpList(ctx context.Context, req mcpListInput) ([]store.Memory, string, error) {
	if req.Limit <= 0 {
		req.Limit = 50
	}
	if req.MaxChars <= 0 {
		req.MaxChars = 4000
	}
	sc, err := resolveMCPScope(scopeOptions{user: req.User, project: req.Project, session: req.Session})
	if err != nil {
		return nil, "", err
	}
	params, err := listParams(sc, memoryFilterOptions{
		since:          req.Since,
		before:         req.Before,
		sourceKind:     req.SourceKind,
		agent:          req.Agent,
		source:         req.Source,
		role:           req.Role,
		claimKey:       req.ClaimKey,
		claimKeyPrefix: req.ClaimKeyPrefix,
		validity:       req.Validity,
		current:        req.Current,
		historical:     req.Historical,
	}, req.Limit, req.IncludeDeleted)
	if err != nil {
		return nil, "", err
	}
	st, _, err := openStore()
	if err != nil {
		return nil, "", err
	}
	defer st.Close()
	memories, err := st.List(ctx, params)
	if err != nil {
		return nil, "", err
	}
	return memories, memoryBlocks(memories, req.MaxChars, false), nil
}

// mcpExport mirrors the CLI export command. The CLI's --current defaults to
// true; over MCP that is the unflagged default, with historical/all_validity as
// the two opt-outs.
func mcpExport(ctx context.Context, req mcpExportInput) (exportResult, string, error) {
	claimKey := strings.TrimSpace(req.ClaimKey)
	claimKeyPrefix := strings.TrimSpace(req.ClaimKeyPrefix)
	if req.Historical && req.AllValidity {
		return exportResult{}, "", fmt.Errorf("choose historical or all_validity, not both")
	}
	lifecycle, err := exportLifecycle(claimKey, claimKeyPrefix, !req.AllValidity, req.Historical)
	if err != nil {
		return exportResult{}, "", err
	}
	sc, err := resolveMCPScope(scopeOptions{user: req.User, project: req.Project, session: req.Session})
	if err != nil {
		return exportResult{}, "", err
	}
	st, _, err := openStore()
	if err != nil {
		return exportResult{}, "", err
	}
	defer st.Close()
	memories, err := st.ExportClaims(ctx, store.ExportParams{
		ScopeKind:      sc.Kind,
		ScopeID:        sc.ID,
		ClaimKey:       claimKey,
		ClaimKeyPrefix: claimKeyPrefix,
		Lifecycle:      lifecycle,
		Limit:          req.Limit,
	})
	if err != nil {
		return exportResult{}, "", err
	}
	result := exportResult{
		ScopeKind:      sc.Kind,
		ScopeID:        sc.ID,
		ClaimKey:       claimKey,
		ClaimKeyPrefix: claimKeyPrefix,
		Lifecycle:      lifecycle,
		Count:          len(memories),
		Memories:       memories,
	}
	return result, renderExport(memories), nil
}

func mcpClaims(ctx context.Context, req mcpClaimsInput) (claimsResult, string, error) {
	claimKey := strings.TrimSpace(req.ClaimKey)
	claimKeyPrefix := strings.TrimSpace(req.ClaimKeyPrefix)
	if err := validateClaimKeyFilters(memoryFilterOptions{claimKey: claimKey, claimKeyPrefix: claimKeyPrefix}); err != nil {
		return claimsResult{}, "", err
	}
	sc, err := resolveMCPScope(scopeOptions{user: req.User, project: req.Project, session: req.Session})
	if err != nil {
		return claimsResult{}, "", err
	}
	st, _, err := openStore()
	if err != nil {
		return claimsResult{}, "", err
	}
	defer st.Close()
	summaries, err := st.ClaimSummaries(ctx, store.ClaimSummaryParams{
		ScopeKind:      sc.Kind,
		ScopeID:        sc.ID,
		ClaimKey:       claimKey,
		ClaimKeyPrefix: claimKeyPrefix,
	})
	if err != nil {
		return claimsResult{}, "", err
	}
	result := claimsResult{ScopeKind: sc.Kind, ScopeID: sc.ID, ClaimKeyPrefix: claimKeyPrefix, Claims: summaries}
	var b strings.Builder
	for _, summary := range summaries {
		fmt.Fprintf(&b, "%s\t%d\t%s\t%s\n", summary.ClaimKey, summary.Count, summary.CurrentValidity, summary.CurrentID)
	}
	return result, b.String(), nil
}

func mcpAdd(ctx context.Context, req mcpAddInput) (addResult, string, error) {
	req.Content = strings.TrimSpace(req.Content)
	if req.Content == "" {
		return addResult{}, "", fmt.Errorf("content is required")
	}
	sc, err := resolveMCPScope(scopeOptions{user: req.User, project: req.Project, session: req.Session})
	if err != nil {
		return addResult{}, "", err
	}
	st, _, err := openStore()
	if err != nil {
		return addResult{}, "", err
	}
	defer st.Close()
	mem, duplicate, err := st.AddMemory(ctx, store.AddMemoryParams{
		Role:         req.Role,
		Content:      req.Content,
		SourceKind:   firstNonEmpty(req.SourceKind, "direct"),
		SourceAgent:  req.Agent,
		SourcePath:   req.SourcePath,
		SourceRef:    req.SourceRef,
		ScopeKind:    sc.Kind,
		ScopeID:      sc.ID,
		ProjectID:    sc.ProjectID,
		SessionID:    sc.SessionID,
		MetadataJSON: req.Metadata,
		Validity:     req.Validity,
		ClaimKey:     req.ClaimKey,
		Supersedes:   req.Supersedes,
		SupersededBy: req.SupersededBy,
	})
	if err != nil {
		return addResult{}, "", err
	}
	result := addResult{Memory: mem, Duplicate: duplicate}
	return result, mem.Content, nil
}

func mcpRemember(ctx context.Context, req mcpRememberInput) (rememberResult, string, error) {
	content := strings.TrimSpace(req.Content)
	if content == "" {
		return rememberResult{}, "", fmt.Errorf("content is required")
	}
	sc, err := resolveMCPScope(scopeOptions{user: req.User, project: req.Project, session: req.Session})
	if err != nil {
		return rememberResult{}, "", err
	}
	st, _, err := openStore()
	if err != nil {
		return rememberResult{}, "", err
	}
	defer st.Close()
	result, err := runRemember(ctx, st, sc, content, rememberOptions{
		role:         req.Role,
		agent:        req.Agent,
		sourcePath:   req.SourcePath,
		sourceRef:    req.SourceRef,
		metadata:     req.Metadata,
		validity:     firstNonEmpty(req.Validity, "active"),
		claimKey:     req.ClaimKey,
		supersedes:   req.Supersedes,
		supersededBy: req.SupersededBy,
	})
	if err != nil {
		return rememberResult{}, "", err
	}
	return result, result.Memory.Content, nil
}

func mcpHandoff(ctx context.Context, req mcpHandoffInput) (handoffResult, string, error) {
	opts := handoffOptions{
		agent:        req.Agent,
		decision:     req.Decisions,
		constraint:   req.Constraints,
		nextStep:     req.NextSteps,
		openQuestion: req.OpenQuestions,
		supersedes:   req.Supersedes,
		claimKey:     firstNonEmpty(req.ClaimKey, "handoff.latest"),
	}
	content := buildHandoffContent(req.Summary, opts)
	if strings.TrimSpace(content) == "" {
		return handoffResult{}, "", fmt.Errorf("handoff content is empty; pass summary, decisions, constraints, next_steps, or open_questions")
	}
	sc, err := resolveMCPScope(scopeOptions{user: req.User, project: req.Project, session: req.Session})
	if err != nil {
		return handoffResult{}, "", err
	}
	st, _, err := openStore()
	if err != nil {
		return handoffResult{}, "", err
	}
	defer st.Close()
	result, err := runHandoff(ctx, st, sc, content, opts)
	if err != nil {
		return handoffResult{}, "", err
	}
	return result, result.Memory.Content, nil
}

func resolveMCPScope(opts scopeOptions) (scope.Scope, error) {
	return resolveScopeWithDefault(nil, opts, "project")
}

func mcpTextResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}
}

func mcpToolResult(kind string, data any, text string, err error) (*mcp.CallToolResult, envelope, error) {
	if err != nil {
		code, _ := classifyError(err)
		payload := errorPayload{Code: code, Message: err.Error()}
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{
				Text: fmt.Sprintf("%s: %s", code, err.Error()),
			}},
		}, newEnvelope("error", payload), nil
	}
	return mcpTextResult(text), newEnvelope(kind, data), nil
}
