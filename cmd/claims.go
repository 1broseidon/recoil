package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/1broseidon/recoil/internal/store"
	"github.com/spf13/cobra"
)

type claimsOptions struct {
	scope scopeOptions
}

type claimsResult struct {
	ScopeKind string               `json:"scope_kind"`
	ScopeID   string               `json:"scope_id"`
	Claims    []store.ClaimSummary `json:"claims"`
}

func newClaimsCommand() *cobra.Command {
	var claimsOpts claimsOptions
	c := &cobra.Command{
		Use:   "claims",
		Short: "List claim-key families in scope",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			sc, err := resolveReadScope(cmd, claimsOpts.scope)
			if err != nil {
				return err
			}
			st, _, err := openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			summaries, err := st.ClaimSummaries(context.Background(), sc.Kind, sc.ID)
			if err != nil {
				return err
			}
			result := claimsResult{ScopeKind: sc.Kind, ScopeID: sc.ID, Claims: summaries}
			w := cmd.OutOrStdout()
			if opts.json {
				return writeJSON(w, "claims_result", result)
			}
			var b strings.Builder
			for _, summary := range summaries {
				fmt.Fprintf(&b, "%s\t%d\t%s\t%s\n", summary.ClaimKey, summary.Count, summary.CurrentValidity, summary.CurrentID)
			}
			return frontmatter(w, []kv{
				{k: "scope", v: sc.Kind},
				{k: "scope_id", v: sc.ID},
				{k: "result_count", v: fmt.Sprintf("%d", len(summaries))},
			}, b.String())
		},
	}
	addScopeFlags(c, &claimsOpts.scope)
	return c
}
