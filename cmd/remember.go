package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/1broseidon/recoil/internal/store"
	"github.com/spf13/cobra"
)

type rememberOptions struct {
	scope        scopeOptions
	file         string
	role         string
	agent        string
	sourcePath   string
	sourceRef    string
	metadata     string
	validity     string
	claimKey     string
	supersedes   string
	supersededBy string
}

type rememberInference struct {
	Role       string `json:"role"`
	ClaimKey   string `json:"claim_key,omitempty"`
	Confidence string `json:"confidence"`
	Reason     string `json:"reason"`
}

type rememberResult struct {
	Memory    *store.Memory     `json:"memory"`
	Duplicate bool              `json:"duplicate"`
	Inference rememberInference `json:"inference"`
}

func newRememberCommand() *cobra.Command {
	var rememberOpts rememberOptions
	c := &cobra.Command{
		Use:   "remember [text]",
		Short: "Remember useful project context with deterministic role inference",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			content, err := readAddInput(rememberOpts.file, args)
			if err != nil {
				return err
			}
			if strings.TrimSpace(content) == "" {
				return fmt.Errorf("memory content is empty")
			}
			inference := inferRemember(content, rememberOpts.role, rememberOpts.claimKey)
			metadata := rememberOpts.metadata
			if metadata == "" {
				data, err := json.Marshal(map[string]any{
					"remember": map[string]any{
						"inferred_role":        inference.Role,
						"inferred_claim_key":   inference.ClaimKey,
						"inference_confidence": inference.Confidence,
						"inference_reason":     inference.Reason,
					},
				})
				if err != nil {
					return err
				}
				metadata = string(data)
			}
			sc, err := resolveScope(cmd, rememberOpts.scope)
			if err != nil {
				return err
			}
			st, _, err := openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			mem, duplicate, err := st.AddMemory(context.Background(), store.AddMemoryParams{
				Role:         inference.Role,
				Content:      content,
				SourceKind:   "direct",
				SourceAgent:  rememberOpts.agent,
				SourcePath:   rememberOpts.sourcePath,
				SourceRef:    rememberOpts.sourceRef,
				ScopeKind:    sc.Kind,
				ScopeID:      sc.ID,
				ProjectID:    sc.ProjectID,
				SessionID:    sc.SessionID,
				MetadataJSON: metadata,
				Validity:     firstNonEmpty(rememberOpts.validity, "active"),
				ClaimKey:     inference.ClaimKey,
				Supersedes:   rememberOpts.supersedes,
				SupersededBy: rememberOpts.supersededBy,
			})
			if err != nil {
				return err
			}
			result := rememberResult{Memory: mem, Duplicate: duplicate, Inference: inference}
			if opts.json {
				return writeJSON(cmd.OutOrStdout(), "remember_result", result)
			}
			return frontmatter(cmd.OutOrStdout(), []kv{
				{k: "id", v: mem.ID},
				{k: "scope", v: mem.ScopeKind},
				{k: "scope_id", v: mem.ScopeID},
				{k: "role", v: mem.Role},
				{k: "claim_key", v: mem.ClaimKey},
				{k: "inference_confidence", v: inference.Confidence},
				{k: "inference_reason", v: inference.Reason},
				{k: "duplicate", v: fmt.Sprintf("%t", duplicate)},
			}, mem.Content)
		},
	}
	addScopeFlags(c, &rememberOpts.scope)
	c.Flags().StringVar(&rememberOpts.file, "file", "", "read memory content from a file, or '-' for stdin")
	c.Flags().StringVar(&rememberOpts.role, "role", "", "override inferred role")
	c.Flags().StringVar(&rememberOpts.agent, "agent", "", "source agent name")
	c.Flags().StringVar(&rememberOpts.sourcePath, "source-path", "", "source file or transcript path")
	c.Flags().StringVar(&rememberOpts.sourceRef, "source-ref", "", "source reference within the path")
	c.Flags().StringVar(&rememberOpts.metadata, "metadata", "", "custom metadata as JSON")
	c.Flags().StringVar(&rememberOpts.validity, "validity", "active", "validity state")
	c.Flags().StringVar(&rememberOpts.claimKey, "claim-key", "", "override inferred claim key")
	c.Flags().StringVar(&rememberOpts.supersedes, "supersedes", "", "memory ID this memory supersedes")
	c.Flags().StringVar(&rememberOpts.supersededBy, "superseded-by", "", "memory ID that supersedes this memory")
	return c
}

func inferRemember(content, roleOverride, claimOverride string) rememberInference {
	text := strings.ToLower(strings.TrimSpace(content))
	role := strings.TrimSpace(roleOverride)
	confidence := "medium"
	reason := "inferred from content"
	if role != "" {
		confidence = "override"
		reason = "role override"
	} else {
		switch {
		case containsAny(text, "next step", "todo", "handoff", "follow up", "open question"):
			role = "handoff"
			confidence = "high"
		case containsAny(text, "must ", "must not", "do not", "never ", "constraint", "required"):
			role = "constraint"
			confidence = "high"
		case containsAny(text, "prefer", "preference", "likes ", "wants "):
			role = "preference"
			confidence = "high"
		case containsAny(text, "decide", "decision", "choose", "chose", "use ", "reject", "switch", "go with", "ship "):
			role = "decision"
			confidence = "high"
		default:
			role = "note"
			confidence = "low"
			reason = "no decision, constraint, preference, or handoff signal; stored as note"
		}
	}
	claim := strings.TrimSpace(claimOverride)
	if claim == "" && rememberClaimRole(role) {
		claim = role + "." + rememberSlug(content)
	}
	return rememberInference{Role: role, ClaimKey: claim, Confidence: confidence, Reason: reason}
}

func rememberClaimRole(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "adr", "decision", "constraint", "preference", "rule", "handoff":
		return true
	default:
		return false
	}
}

func containsAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

var rememberWordRE = regexp.MustCompile(`[a-z0-9]+`)

func rememberSlug(content string) string {
	words := rememberWordRE.FindAllString(strings.ToLower(content), -1)
	stop := map[string]bool{
		"a": true, "an": true, "and": true, "are": true, "as": true, "because": true,
		"be": true, "by": true, "for": true, "from": true, "in": true, "is": true,
		"it": true, "of": true, "on": true, "or": true, "should": true, "that": true,
		"the": true, "this": true, "to": true, "use": true, "we": true, "with": true,
	}
	parts := make([]string, 0, 4)
	for _, word := range words {
		if stop[word] || len(word) < 3 {
			continue
		}
		parts = append(parts, word)
		if len(parts) == 4 {
			break
		}
	}
	if len(parts) == 0 {
		return "memory"
	}
	return strings.Join(parts, "-")
}
