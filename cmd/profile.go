package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/1broseidon/recoil/internal/store"
	"github.com/spf13/cobra"
)

type profileOptions struct {
	scope  scopeOptions
	entity string
	limit  int
}

type profileResult struct {
	Entity      string        `json:"entity"`
	Memory      *store.Memory `json:"memory"`
	Superseded  []string      `json:"superseded,omitempty"`
	EvidenceIDs []string      `json:"evidence_ids,omitempty"`
}

func newProfileCommand() *cobra.Command {
	var profileOpts profileOptions
	c := &cobra.Command{
		Use:   "profile",
		Short: "Build a deterministic entity profile from current memories",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			entity := strings.TrimSpace(profileOpts.entity)
			if entity == "" {
				return fmt.Errorf("--entity is required")
			}
			if profileOpts.limit <= 0 {
				profileOpts.limit = 20
			}
			sc, err := resolveReadScope(cmd, profileOpts.scope)
			if err != nil {
				return err
			}
			st, _, err := openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			ctx := context.Background()
			params := store.SearchParams{
				Query:        entity,
				ScopeKind:    sc.Kind,
				ScopeID:      sc.ID,
				Limit:        profileOpts.limit,
				Lifecycle:    store.LifecycleCurrent,
				SignalRerank: true,
			}
			rows, err := runRetriever(ctx, st, params, retrieverOptions{
				mode:  retrievalFTS,
				limit: profileOpts.limit,
			})
			if err != nil {
				return err
			}
			var evidence []store.Memory
			for _, row := range rows {
				if row.SourceKind == "entity_profile" {
					continue
				}
				if !memoryMentionsEntity(row, entity) {
					continue
				}
				evidence = append(evidence, row)
			}
			if len(evidence) == 0 {
				return fmt.Errorf("no current evidence found for entity %q", entity)
			}
			claimKey := "entity:" + profileKey(entity)
			content := buildEntityProfileContent(entity, evidence)
			mem, duplicate, err := st.AddMemory(ctx, store.AddMemoryParams{
				Role:       "profile",
				Content:    content,
				SourceKind: "entity_profile",
				SourcePath: "entity/" + entity,
				SourceRef:  entity,
				ScopeKind:  sc.Kind,
				ScopeID:    sc.ID,
				ProjectID:  sc.ProjectID,
				Validity:   "active",
				ClaimKey:   claimKey,
			})
			if err != nil {
				return err
			}
			var superseded []string
			if !duplicate {
				existing, err := st.List(ctx, store.ListParams{
					ScopeKind: sc.Kind,
					ScopeID:   sc.ID,
					ClaimKey:  claimKey,
					Lifecycle: store.LifecycleCurrent,
					Limit:     50,
				})
				if err != nil {
					return err
				}
				for _, old := range existing {
					if old.ID == mem.ID {
						continue
					}
					_, err := st.UpdateLifecycle(ctx, store.LifecycleParams{
						IDOrPrefix:   old.ID,
						Validity:     "superseded",
						ClaimKey:     claimKey,
						SupersededBy: mem.ID,
					})
					if err != nil {
						return err
					}
					superseded = append(superseded, old.ID)
				}
			}
			var evidenceIDs []string
			for _, row := range evidence {
				evidenceIDs = append(evidenceIDs, row.ID)
			}
			result := profileResult{Entity: entity, Memory: mem, Superseded: superseded, EvidenceIDs: evidenceIDs}
			if opts.json {
				return writeJSON(cmd.OutOrStdout(), "profile_result", result)
			}
			return frontmatter(cmd.OutOrStdout(), []kv{
				{k: "entity", v: entity},
				{k: "memory_id", v: mem.ID},
				{k: "claim_key", v: claimKey},
				{k: "evidence_count", v: fmt.Sprintf("%d", len(evidenceIDs))},
				{k: "superseded_count", v: fmt.Sprintf("%d", len(superseded))},
			}, content)
		},
	}
	addScopeFlags(c, &profileOpts.scope)
	c.Flags().StringVar(&profileOpts.entity, "entity", "", "entity or topic name to profile")
	c.Flags().IntVar(&profileOpts.limit, "limit", 20, "maximum current memories to use as profile evidence")
	return c
}

func memoryMentionsEntity(mem store.Memory, entity string) bool {
	entity = strings.ToLower(strings.TrimSpace(entity))
	if entity == "" {
		return false
	}
	text := strings.ToLower(strings.Join([]string{mem.Content, mem.SourcePath, mem.SourceRef, mem.ClaimKey}, " "))
	return strings.Contains(text, entity)
}

func buildEntityProfileContent(entity string, evidence []store.Memory) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Profile - %s:\n", entity)
	seen := map[string]bool{}
	for _, mem := range evidence {
		line := strings.TrimSpace(oneLineForProfile(mem.Content))
		if line == "" || seen[line] {
			continue
		}
		seen[line] = true
		fmt.Fprintf(&b, "- %s", line)
		if mem.SourceRef != "" || mem.SourcePath != "" {
			fmt.Fprintf(&b, " (source: %s %s)", mem.SourcePath, mem.SourceRef)
		}
		b.WriteByte('\n')
	}
	return strings.TrimSpace(b.String())
}

func oneLineForProfile(value string) string {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return ""
	}
	out := strings.Join(fields, " ")
	if len(out) > 360 {
		out = strings.TrimSpace(out[:357]) + "..."
	}
	return out
}

func profileKey(entity string) string {
	key := strings.ToLower(strings.TrimSpace(entity))
	key = strings.NewReplacer(" ", "-", "/", "-", "\\", "-", ":", "-").Replace(key)
	return key
}
