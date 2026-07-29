package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/1broseidon/recoil/internal/store"
	"github.com/spf13/cobra"
)

type claimsOptions struct {
	scope   scopeOptions
	filters memoryFilterOptions
}

type claimsResult struct {
	ScopeKind      string               `json:"scope_kind"`
	ScopeID        string               `json:"scope_id"`
	ClaimKeyPrefix string               `json:"claim_key_prefix,omitempty"`
	Claims         []store.ClaimSummary `json:"claims"`
}

func newClaimsCommand() *cobra.Command {
	var claimsOpts claimsOptions
	c := &cobra.Command{
		Use:   "claims",
		Short: "List claim-key families in scope",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateClaimKeyFilters(claimsOpts.filters); err != nil {
				return err
			}
			sc, err := resolveReadScope(cmd, claimsOpts.scope)
			if err != nil {
				return err
			}
			claimKey := strings.TrimSpace(claimsOpts.filters.claimKey)
			claimKeyPrefix := strings.TrimSpace(claimsOpts.filters.claimKeyPrefix)
			st, _, err := openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			summaries, err := st.ClaimSummaries(context.Background(), store.ClaimSummaryParams{
				ScopeKind:      sc.Kind,
				ScopeID:        sc.ID,
				ClaimKey:       claimKey,
				ClaimKeyPrefix: claimKeyPrefix,
			})
			if err != nil {
				return err
			}
			result := claimsResult{ScopeKind: sc.Kind, ScopeID: sc.ID, ClaimKeyPrefix: claimKeyPrefix, Claims: summaries}
			w := cmd.OutOrStdout()
			if opts.json {
				return writeJSON(w, "claims_result", result)
			}
			var b strings.Builder
			for _, summary := range summaries {
				fmt.Fprintf(&b, "%s\t%d\t%s\t%s\n", summary.ClaimKey, summary.Count, summary.CurrentValidity, summary.CurrentID)
			}
			meta := []kv{
				{k: "scope", v: sc.Kind},
				{k: "scope_id", v: sc.ID},
			}
			if claimKey != "" {
				meta = append(meta, kv{k: "claim_key", v: claimKey})
			}
			if claimKeyPrefix != "" {
				meta = append(meta, kv{k: "claim_key_prefix", v: claimKeyPrefix})
			}
			meta = append(meta, kv{k: "result_count", v: fmt.Sprintf("%d", len(summaries))})
			return frontmatter(w, meta, b.String())
		},
	}
	addScopeFlags(c, &claimsOpts.scope)
	c.Flags().StringVar(&claimsOpts.filters.claimKey, "claim-key", "", "filter to one exact claim key")
	addClaimKeyPrefixFlag(c, &claimsOpts.filters)
	return c
}
