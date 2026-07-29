package cmd

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/1broseidon/recoil/internal/agentsessions"
	"github.com/1broseidon/recoil/internal/config"
	"github.com/1broseidon/recoil/internal/sessionevidence"
	"github.com/spf13/cobra"
)

type sessionBackfillOptions struct {
	scope    scopeOptions
	agents   []string
	minChars int
	force    bool
	reingest bool
	minimal  bool
}

// activeSessionWindow leaves a still-being-written transcript to the live hook
// path. Backfilling an active session re-ingests it on every run as it grows,
// producing near-duplicate memories; backfill is for closed history.
const activeSessionWindow = 2 * time.Minute

func newSessionEvidenceDiscoverCommand() *cobra.Command {
	var o sessionBackfillOptions
	c := &cobra.Command{
		Use:   "discover",
		Short: "List other agents' sessions bound to this repo (no writes)",
		Long: `Discover finds prior sessions from other coding agents (Claude Code,
Codex) that ran in this workspace, by matching each transcript's recorded cwd
against the repo root. It writes nothing — it is the dry run for backfill.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			sc, err := resolveReadScope(cmd, o.scope)
			if err != nil {
				return err
			}
			adapters, err := agentsessions.Adapters(o.agents...)
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			type row struct {
				agent, session, modtime, path string
			}
			var rows []row
			counts := map[string]int{}
			var all []agentsessions.SessionRef
			for _, a := range adapters {
				refs, err := a.Discover(sc.Root)
				if err != nil {
					return fmt.Errorf("%s discover: %w", a.Name(), err)
				}
				counts[a.Name()] = len(refs)
				all = append(all, refs...)
				for _, ref := range refs {
					session := ref.SessionID
					if ref.NativeSessionID != "" && ref.NativeSessionID != ref.SessionID {
						session = ref.SessionID + " native=" + ref.NativeSessionID
					}
					if ref.Branch != "" {
						session += " branch=" + ref.Branch
					}
					rows = append(rows, row{ref.Agent, session, ref.ModTime.Format(time.RFC3339), ref.Path})
				}
			}
			if opts.json {
				return writeJSON(w, "session_discover_result", map[string]any{
					"scope":    sc.Kind,
					"scope_id": sc.ID,
					"repo":     sc.Root,
					"counts":   counts,
					"sessions": all,
				})
			}
			sort.SliceStable(rows, func(i, j int) bool { return rows[i].modtime < rows[j].modtime })
			var lines []string
			for _, r := range rows {
				line := fmt.Sprintf("%s\t%s\t%s\t%s", r.agent, r.modtime, r.session, r.path)
				if o.minimal {
					fmt.Fprintln(w, line)
				} else {
					lines = append(lines, line)
				}
			}
			if o.minimal {
				return nil
			}
			return frontmatter(w, []kv{
				{k: "repo", v: sc.Root},
				{k: "claude", v: fmt.Sprintf("%d", counts["claude"])},
				{k: "codex", v: fmt.Sprintf("%d", counts["codex"])},
				{k: "total", v: fmt.Sprintf("%d", len(rows))},
			}, strings.Join(lines, "\n"))
		},
	}
	addScopeFlags(c, &o.scope)
	c.Flags().StringSliceVar(&o.agents, "agent", nil, "limit to these agents (claude,codex); default all")
	c.Flags().BoolVar(&o.minimal, "minimal", false, "print tab-separated rows")
	return c
}

func newSessionEvidenceBackfillCommand() *cobra.Command {
	var o sessionBackfillOptions
	includePersonalFacts := false
	c := &cobra.Command{
		Use:   "backfill",
		Short: "Ingest other agents' prior sessions into the evidence tier",
		Long: `Backfill discovers prior Claude Code and Codex sessions for this repo and
runs each through the same selective evidence pipeline as the live hook:
load-bearing turns are selected, redacted, stamped with their source agent and
original timestamp, and mined into searchable memory. Unchanged sessions are
skipped on re-runs via a per-scope cursor.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			sc, err := resolveScope(cmd, o.scope)
			if err != nil {
				return err
			}
			settings, err := effectiveSettings()
			if err != nil {
				return err
			}
			if !o.force && !settings.Bool("session-evidence.enabled", false) {
				return fmt.Errorf("session evidence is disabled; run `recoil config set session-evidence.enabled true` or pass --force")
			}
			adapters, err := agentsessions.Adapters(o.agents...)
			if err != nil {
				return err
			}
			stateDir, err := config.ResolveStateDir()
			if err != nil {
				return err
			}
			cursors, err := agentsessions.LoadCursors(stateDir, sc.ID)
			if err != nil {
				return err
			}
			captured, err := sessionevidence.CapturedNativeSessions(stateDir, sc.ID)
			if err != nil {
				return err
			}
			minChars := effectiveMinChars(settings, o.minChars)

			st, _, err := openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			ctx := context.Background()
			now := time.Now().UTC()

			type agentStat struct {
				discovered, ingested, skipped, selected, added, duplicates int
			}
			stats := map[string]*agentStat{}
			for _, a := range adapters {
				stat := &agentStat{}
				stats[a.Name()] = stat
				refs, err := a.Discover(sc.Root)
				if err != nil {
					return fmt.Errorf("%s discover: %w", a.Name(), err)
				}
				stat.discovered = len(refs)
				for _, ref := range refs {
					if !o.reingest && now.Sub(ref.ModTime) < activeSessionWindow {
						stat.skipped++
						continue
					}
					if !o.reingest && captured[sessionevidence.NativeSessionKey(ref.Agent, firstNonEmpty(ref.NativeSessionID, ref.SessionID))] && !cursors.SeenNative(ref) {
						stat.skipped++
						continue
					}
					if !o.reingest && cursors.Unchanged(ref) {
						stat.skipped++
						continue
					}
					turns, err := a.Read(ref)
					if err != nil {
						return fmt.Errorf("%s read %s: %w", a.Name(), ref.SessionID, err)
					}
					result, err := sessionevidence.IngestTurns(turns, sessionevidence.Options{
						StateDir:             stateDir,
						ScopeKind:            sc.Kind,
						ScopeID:              sc.ID,
						SourceAgent:          ref.Agent,
						SessionID:            ref.SessionID,
						NativeSessionID:      ref.NativeSessionID,
						Branch:               ref.Branch,
						RepoRoot:             sc.Root,
						MinChars:             minChars,
						Now:                  now,
						ExcludePersonalFacts: !includePersonalFacts,
					})
					if err != nil {
						return fmt.Errorf("%s ingest %s: %w", a.Name(), ref.SessionID, err)
					}
					cursors.Record(ref, result.Selected, now)
					stat.ingested++
					stat.selected += result.Selected
					if result.Selected > 0 {
						mined, err := mineSessionEvidence(ctx, st, sc, mineOptions{
							scope: o.scope,
							role:  "source",
							agent: ref.Agent,
						}, result.SourcePath)
						if err != nil {
							return fmt.Errorf("%s mine %s: %w", a.Name(), ref.SessionID, err)
						}
						stat.added += mined.Added
						stat.duplicates += mined.Duplicates
					}
				}
			}
			if err := cursors.Save(); err != nil {
				return err
			}

			w := cmd.OutOrStdout()
			if opts.json {
				return writeJSON(w, "session_backfill_result", map[string]any{
					"scope":    sc.Kind,
					"scope_id": sc.ID,
					"repo":     sc.Root,
					"agents":   stats,
				})
			}
			names := make([]string, 0, len(stats))
			for name := range stats {
				names = append(names, name)
			}
			sort.Strings(names)
			var lines []string
			var totalAdded int
			for _, name := range names {
				s := stats[name]
				totalAdded += s.added
				lines = append(lines, fmt.Sprintf("%s\tdiscovered=%d ingested=%d skipped=%d selected=%d added=%d duplicates=%d",
					name, s.discovered, s.ingested, s.skipped, s.selected, s.added, s.duplicates))
			}
			return frontmatter(w, []kv{
				{k: "repo", v: sc.Root},
				{k: "memories_added", v: fmt.Sprintf("%d", totalAdded)},
			}, strings.Join(lines, "\n"))
		},
	}
	addScopeFlags(c, &o.scope)
	c.Flags().StringSliceVar(&o.agents, "agent", nil, "limit to these agents (claude,codex); default all")
	c.Flags().IntVar(&o.minChars, "min-chars", 0, "minimum chars for non-directive evidence")
	c.Flags().BoolVar(&o.force, "force", false, "backfill even when session evidence is disabled")
	c.Flags().BoolVar(&o.reingest, "reingest", false, "re-ingest sessions even if unchanged since last run")
	c.Flags().BoolVar(&includePersonalFacts, "include-personal-facts", false, "include personal_fact evidence during bulk backfill")
	return c
}
