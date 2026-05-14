package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/1broseidon/recoil/internal/store"
	"github.com/spf13/cobra"
)

type decideOptions struct {
	scope        scopeOptions
	file         string
	role         string
	agent        string
	sourcePath   string
	sourceRef    string
	metadata     string
	claimKey     string
	supersedes   string
	supersededBy string
	validity     string
	predicate    predicateOptions
	stance       string
	subject      string
}

func newDecideCommand() *cobra.Command {
	var decideOpts decideOptions
	c := &cobra.Command{
		Use:   "decide [text]",
		Short: "Add a decision memory with a required claim key",
		Long: `Add a structured decision memory with a stable claim key.

Date-bound decisions should use --valid-until <date>; recoil stores that as
predicate.kind=valid_until. The legacy predicate kind date_expiry is accepted
only as a write-time alias and normalizes to valid_until.

--predicate fields and --holds-while compose: recoil evaluates the narrow
deterministic predicate when it can, while holds_while remains advisory context
for the agent/operator to recheck. Use --stance and --subject together when the
decision should participate in contradiction detection.`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(decideOpts.claimKey) == "" {
				return fmt.Errorf("--claim-key is required")
			}
			content, err := readAddInput(decideOpts.file, args)
			if err != nil {
				return err
			}
			if strings.TrimSpace(content) == "" {
				return fmt.Errorf("decision content is empty")
			}
			predicate, hasPredicate, err := buildDecisionPredicate(decideOpts.predicate)
			if err != nil {
				return err
			}
			metadata := decideOpts.metadata
			if strings.TrimSpace(decideOpts.stance) != "" || strings.TrimSpace(decideOpts.subject) != "" {
				if strings.TrimSpace(decideOpts.stance) == "" || strings.TrimSpace(decideOpts.subject) == "" {
					return fmt.Errorf("--stance and --subject must be provided together")
				}
				metadata, err = mergeDecisionMetadata(metadata, decideOpts.stance, decideOpts.subject)
				if err != nil {
					return err
				}
			}
			if hasPredicate {
				metadata, err = mergePredicateMetadata(metadata, predicate)
				if err != nil {
					return err
				}
			}
			sc, err := resolveScope(cmd, decideOpts.scope)
			if err != nil {
				return err
			}
			st, _, err := openStore()
			if err != nil {
				return err
			}
			defer st.Close()

			role := strings.TrimSpace(decideOpts.role)
			if role == "" {
				role = "decision"
			}
			mem, duplicate, err := st.AddMemory(context.Background(), store.AddMemoryParams{
				Role:         role,
				Content:      content,
				SourceAgent:  decideOpts.agent,
				SourcePath:   decideOpts.sourcePath,
				SourceRef:    decideOpts.sourceRef,
				ScopeKind:    sc.Kind,
				ScopeID:      sc.ID,
				ProjectID:    sc.ProjectID,
				SessionID:    sc.SessionID,
				MetadataJSON: metadata,
				Validity:     decideOpts.validity,
				ClaimKey:     decideOpts.claimKey,
				Supersedes:   decideOpts.supersedes,
				SupersededBy: decideOpts.supersededBy,
			})
			if err != nil {
				return err
			}

			w := cmd.OutOrStdout()
			if opts.json {
				return writeJSON(w, "decide_result", addResult{Memory: mem, Duplicate: duplicate})
			}
			return frontmatter(w, []kv{
				{k: "id", v: mem.ID},
				{k: "scope", v: mem.ScopeKind},
				{k: "scope_id", v: mem.ScopeID},
				{k: "created", v: mem.CreatedAt},
				{k: "role", v: mem.Role},
				{k: "validity", v: mem.Validity},
				{k: "claim_key", v: mem.ClaimKey},
				{k: "stance", v: strings.TrimSpace(decideOpts.stance)},
				{k: "subject", v: strings.TrimSpace(decideOpts.subject)},
				{k: "predicate_status", v: predicateStatusForFrontmatter(hasPredicate, predicate)},
				{k: "supersedes", v: mem.Supersedes},
				{k: "superseded_by", v: mem.SupersededBy},
				{k: "duplicate", v: fmt.Sprintf("%t", duplicate)},
			}, mem.Content)
		},
	}
	addScopeFlags(c, &decideOpts.scope)
	c.Flags().StringVar(&decideOpts.file, "file", "", "read decision content from a file, or '-' for stdin")
	c.Flags().StringVar(&decideOpts.role, "role", "decision", "role assigned to the decision memory")
	c.Flags().StringVar(&decideOpts.agent, "agent", "", "source agent name")
	c.Flags().StringVar(&decideOpts.sourcePath, "source-path", "", "source file or transcript path")
	c.Flags().StringVar(&decideOpts.sourceRef, "source-ref", "", "source reference within the path")
	c.Flags().StringVar(&decideOpts.metadata, "metadata", "", "custom metadata as JSON")
	c.Flags().StringVar(&decideOpts.validity, "validity", "active", "validity state for the decision")
	c.Flags().StringVar(&decideOpts.claimKey, "claim-key", "", "stable claim family for supersession")
	c.Flags().StringVar(&decideOpts.stance, "stance", "", "decision stance: prefers, rejects, requires, forbids")
	c.Flags().StringVar(&decideOpts.subject, "subject", "", "decision subject used by check opposition detection")
	c.Flags().StringVar(&decideOpts.supersedes, "supersedes", "", "memory ID this decision supersedes")
	c.Flags().StringVar(&decideOpts.supersededBy, "superseded-by", "", "memory ID that supersedes this decision")
	addPredicateFlags(c.Flags(), &decideOpts.predicate)
	return c
}

func predicateStatusForFrontmatter(hasPredicate bool, pred decisionPredicate) string {
	if !hasPredicate {
		return "unknown"
	}
	return firstNonEmpty(pred.Status, "unknown")
}
