package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/1broseidon/recoil/internal/scope"
	"github.com/1broseidon/recoil/internal/store"
	"github.com/spf13/cobra"
)

type handoffOptions struct {
	scope        scopeOptions
	file         string
	agent        string
	decision     []string
	constraint   []string
	nextStep     []string
	openQuestion []string
	supersedes   string
	claimKey     string
	noSupersede  bool
}

type handoffResult struct {
	Memory         *store.Memory `json:"memory"`
	AutoSuperseded []string      `json:"auto_superseded,omitempty"`
}

func newHandoffCommand() *cobra.Command {
	var handoffOpts handoffOptions
	c := &cobra.Command{
		Use:   "handoff [summary]",
		Short: "Close a session with structured next context for the next agent",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			content, err := readAddInputAllowEmpty(handoffOpts.file, args)
			if err != nil {
				return err
			}
			content = buildHandoffContent(content, handoffOpts)
			if strings.TrimSpace(content) == "" {
				return fmt.Errorf("handoff content is empty; pass text or --decision/--constraint/--next-step")
			}
			sc, err := resolveScope(cmd, handoffOpts.scope)
			if err != nil {
				return err
			}
			st, _, err := openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			ctx := context.Background()
			result, err := runHandoff(ctx, st, sc, content, handoffOpts)
			if err != nil {
				return err
			}
			if opts.json {
				return writeJSON(cmd.OutOrStdout(), "handoff_result", result)
			}
			mem := result.Memory
			meta := []kv{
				{k: "id", v: mem.ID},
				{k: "scope", v: mem.ScopeKind},
				{k: "scope_id", v: mem.ScopeID},
				{k: "role", v: mem.Role},
				{k: "claim_key", v: mem.ClaimKey},
				{k: "supersedes", v: mem.Supersedes},
				{k: "auto_superseded", v: strings.Join(result.AutoSuperseded, ",")},
			}
			return frontmatter(cmd.OutOrStdout(), meta, mem.Content)
		},
	}
	addScopeFlags(c, &handoffOpts.scope)
	c.Flags().StringVar(&handoffOpts.file, "file", "", "read handoff summary from a file, or '-' for stdin")
	c.Flags().StringVar(&handoffOpts.agent, "agent", "", "source agent name")
	c.Flags().StringArrayVar(&handoffOpts.decision, "decision", nil, "decision made this session; repeatable")
	c.Flags().StringArrayVar(&handoffOpts.constraint, "constraint", nil, "constraint discovered this session; repeatable")
	c.Flags().StringArrayVar(&handoffOpts.nextStep, "next-step", nil, "next step for the next session; repeatable")
	c.Flags().StringArrayVar(&handoffOpts.openQuestion, "open-question", nil, "open question for the next session; repeatable")
	c.Flags().StringVar(&handoffOpts.supersedes, "supersedes", "", "previous handoff memory this handoff supersedes")
	c.Flags().StringVar(&handoffOpts.claimKey, "claim-key", "handoff.latest", "claim key for the handoff memory")
	c.Flags().BoolVar(&handoffOpts.noSupersede, "no-supersede", false, "skip automatic supersession of prior active handoffs in the claim family")
	return c
}

func runHandoff(ctx context.Context, st *store.Store, sc scope.Scope, content string, opts handoffOptions) (handoffResult, error) {
	mem, duplicate, err := st.AddMemory(ctx, store.AddMemoryParams{
		Role:        "handoff",
		Content:     content,
		SourceKind:  "direct",
		SourceAgent: opts.agent,
		ScopeKind:   sc.Kind,
		ScopeID:     sc.ID,
		ProjectID:   sc.ProjectID,
		SessionID:   sc.SessionID,
		Validity:    "active",
		ClaimKey:    firstNonEmpty(strings.TrimSpace(opts.claimKey), "handoff.latest"),
		Supersedes:  opts.supersedes,
	})
	if err != nil {
		return handoffResult{}, err
	}
	var autoSuperseded []string
	if !duplicate && !opts.noSupersede && strings.TrimSpace(opts.supersedes) == "" {
		autoSuperseded, err = autoSupersedeClaimFamily(ctx, st, sc, mem.ClaimKey, mem.ID)
		if err != nil {
			return handoffResult{}, err
		}
		if len(autoSuperseded) == 1 {
			params := lifecycleParamsFromMemory(*mem)
			params.Supersedes = autoSuperseded[0]
			mem, err = st.UpdateLifecycle(ctx, params)
			if err != nil {
				return handoffResult{}, err
			}
		}
	}
	return handoffResult{Memory: mem, AutoSuperseded: autoSuperseded}, nil
}

func readAddInputAllowEmpty(file string, args []string) (string, error) {
	if file != "" || len(args) > 0 {
		return readAddInput(file, args)
	}
	return "", nil
}

func buildHandoffContent(summary string, opts handoffOptions) string {
	var b strings.Builder
	if strings.TrimSpace(summary) != "" {
		b.WriteString(strings.TrimSpace(summary))
		b.WriteString("\n\n")
	}
	writeSection := func(title string, items []string) {
		if len(items) == 0 {
			return
		}
		fmt.Fprintf(&b, "## %s\n", title)
		for _, item := range items {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			fmt.Fprintf(&b, "- %s\n", item)
		}
		b.WriteByte('\n')
	}
	writeSection("Decisions Made", opts.decision)
	writeSection("Constraints Discovered", opts.constraint)
	writeSection("Next Steps", opts.nextStep)
	writeSection("Open Questions", opts.openQuestion)
	return strings.TrimSpace(b.String())
}
