package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/1broseidon/recoil/internal/store"
	"github.com/spf13/cobra"
)

type exportOptions struct {
	scope          scopeOptions
	claimKey       string
	claimKeyPrefix string
	current        bool
	historical     bool
	limit          int
}

type exportResult struct {
	ScopeKind      string         `json:"scope_kind"`
	ScopeID        string         `json:"scope_id"`
	ClaimKey       string         `json:"claim_key,omitempty"`
	ClaimKeyPrefix string         `json:"claim_key_prefix,omitempty"`
	Lifecycle      string         `json:"lifecycle"`
	Count          int            `json:"count"`
	Memories       []store.Memory `json:"memories"`
}

func newExportCommand() *cobra.Command {
	var exportOpts exportOptions
	c := &cobra.Command{
		Use:   "export",
		Short: "Export whole claim families verbatim for doctrine injection",
		Long: `Export claim-keyed memories with no ranking and no truncation.

Export is the deterministic counterpart to wake and search. It does not rank,
does not synthesize a query, does not refresh sources, does not
truncate bodies, and applies no time-dependent priors. Output order is
claim_key ascending, then created_at descending within a key, so two runs over
unchanged store contents are byte-for-byte identical.

One of --claim-key or --claim-key-prefix is required: export is addressed by
claim family, not by relevance. An empty result is a count of 0 and exit 0.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			claimKey := strings.TrimSpace(exportOpts.claimKey)
			claimKeyPrefix := strings.TrimSpace(exportOpts.claimKeyPrefix)
			lifecycle, err := exportLifecycle(claimKey, claimKeyPrefix, exportOpts.current, exportOpts.historical)
			if err != nil {
				return err
			}
			sc, err := resolveReadScope(cmd, exportOpts.scope)
			if err != nil {
				return err
			}
			st, _, err := openReadStore()
			if err != nil {
				return err
			}
			defer st.Close()

			memories, err := st.ExportClaims(context.Background(), store.ExportParams{
				ScopeKind:      sc.Kind,
				ScopeID:        sc.ID,
				ClaimKey:       claimKey,
				ClaimKeyPrefix: claimKeyPrefix,
				Lifecycle:      lifecycle,
				Limit:          exportOpts.limit,
			})
			if err != nil {
				return err
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
			w := cmd.OutOrStdout()
			if opts.json {
				return writeJSON(w, "export_result", result)
			}
			return frontmatter(w, exportFrontmatter(result), renderExport(memories))
		},
	}
	addScopeFlags(c, &exportOpts.scope)
	c.Flags().StringVar(&exportOpts.claimKey, "claim-key", "", "export one exact claim family")
	c.Flags().StringVar(&exportOpts.claimKeyPrefix, "claim-key-prefix", "", "export every claim family under a prefix, e.g. voice.")
	c.Flags().BoolVar(&exportOpts.current, "current", true, "export only current memories")
	c.Flags().BoolVar(&exportOpts.historical, "historical", false, "include past memories instead of only current ones")
	c.Flags().IntVar(&exportOpts.limit, "limit", 0, "maximum memories to export (0 means every match)")
	return c
}

// exportLifecycle resolves the three-way lifecycle selection. --current defaults
// to true, so the useful combinations are: default (current only), --historical
// (past only), and --current=false without --historical (everything).
func exportLifecycle(claimKey, claimKeyPrefix string, current, historical bool) (string, error) {
	if claimKey == "" && claimKeyPrefix == "" {
		return "", fmt.Errorf("provide --claim-key or --claim-key-prefix")
	}
	if claimKey != "" && claimKeyPrefix != "" {
		return "", fmt.Errorf("choose --claim-key or --claim-key-prefix, not both")
	}
	if historical {
		return store.LifecycleHistorical, nil
	}
	if current {
		return store.LifecycleCurrent, nil
	}
	return store.LifecycleAny, nil
}

func exportFrontmatter(result exportResult) []kv {
	meta := []kv{
		{k: "kind", v: "export"},
		{k: "scope", v: result.ScopeKind},
		{k: "scope_id", v: result.ScopeID},
	}
	if result.ClaimKey != "" {
		meta = append(meta, kv{k: "claim_key", v: result.ClaimKey})
	}
	if result.ClaimKeyPrefix != "" {
		meta = append(meta, kv{k: "claim_key_prefix", v: result.ClaimKeyPrefix})
	}
	return append(meta,
		kv{k: "lifecycle", v: result.Lifecycle},
		kv{k: "count", v: fmt.Sprintf("%d", result.Count)},
	)
}

// renderExport writes one section per memory with the FULL body. Unlike
// memoryBlocks it has no character budget, so nothing is ever truncated and the
// output depends only on store contents.
func renderExport(memories []store.Memory) string {
	var b strings.Builder
	for _, mem := range memories {
		fmt.Fprintf(&b, "## %s\n", mem.ClaimKey)
		fmt.Fprintf(&b, "id: %s\n", mem.ID)
		fmt.Fprintf(&b, "created: %s\n", mem.CreatedAt)
		fmt.Fprintf(&b, "validity: %s\n", mem.Validity)
		if mem.Role != "" {
			fmt.Fprintf(&b, "role: %s\n", mem.Role)
		}
		if mem.SourceKind != "" {
			fmt.Fprintf(&b, "source_kind: %s\n", mem.SourceKind)
		}
		if mem.SourceAgent != "" {
			fmt.Fprintf(&b, "source_agent: %s\n", mem.SourceAgent)
		}
		if mem.SourcePath != "" {
			fmt.Fprintf(&b, "source_path: %s\n", mem.SourcePath)
		}
		if mem.SourceRef != "" {
			fmt.Fprintf(&b, "source_ref: %s\n", mem.SourceRef)
		}
		if mem.Supersedes != "" {
			fmt.Fprintf(&b, "supersedes: %s\n", mem.Supersedes)
		}
		if mem.SupersededBy != "" {
			fmt.Fprintf(&b, "superseded_by: %s\n", mem.SupersededBy)
		}
		fmt.Fprintf(&b, "\n%s\n\n", mem.Content)
	}
	if b.Len() == 0 {
		return ""
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}
