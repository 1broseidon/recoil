package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/1broseidon/recoil/internal/config"
	"github.com/1broseidon/recoil/internal/scope"
	"github.com/1broseidon/recoil/internal/sessionevidence"
	"github.com/1broseidon/recoil/internal/store"
	"github.com/spf13/cobra"
)

type sessionEvidenceOptions struct {
	scope    scopeOptions
	file     string
	agent    string
	session  string
	minChars int
	force    bool
	noMine   bool
	minimal  bool
}

type sessionEvidenceIngestResult struct {
	sessionevidence.IngestResult
	Mine *mineResult `json:"mine,omitempty"`
}

func newSessionEvidenceCommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "session-evidence",
		Short: "Capture and manage selected session evidence",
		Long: `Session Evidence captures selected, redacted, load-bearing evidence
from agent sessions. It stores compact evidence slices, not raw transcripts.`,
	}
	c.AddCommand(newSessionEvidenceIngestCommand())
	c.AddCommand(newSessionEvidenceListCommand())
	c.AddCommand(newSessionEvidenceShowCommand())
	c.AddCommand(newSessionEvidenceForgetCommand())
	return c
}

func newSessionEvidenceIngestCommand() *cobra.Command {
	var seOpts sessionEvidenceOptions
	c := &cobra.Command{
		Use:   "ingest",
		Short: "Ingest a runtime transcript payload as compact session evidence",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			sc, err := resolveScope(cmd, seOpts.scope)
			if err != nil {
				return err
			}
			settings, err := effectiveSettings()
			if err != nil {
				return err
			}
			if !seOpts.force && !settings.Bool("session-evidence.enabled", false) {
				return fmt.Errorf("session evidence is disabled; run `recoil config set session-evidence.enabled true` or pass --force")
			}
			data, err := readSessionEvidenceInput(seOpts.file)
			if err != nil {
				return err
			}
			stateDir, err := config.ResolveStateDir()
			if err != nil {
				return err
			}
			result, err := sessionevidence.Ingest(data, sessionevidence.Options{
				StateDir:    stateDir,
				ScopeKind:   sc.Kind,
				ScopeID:     sc.ID,
				SourceAgent: seOpts.agent,
				SessionID:   seOpts.session,
				MinChars:    effectiveMinChars(settings, seOpts.minChars),
			})
			if err != nil {
				return err
			}
			full := sessionEvidenceIngestResult{IngestResult: result}
			if !seOpts.noMine && result.Selected > 0 {
				st, _, err := openStore()
				if err != nil {
					return err
				}
				defer st.Close()
				mined, err := mineSessionEvidence(context.Background(), st, sc, mineOptions{
					scope:  seOpts.scope,
					role:   "source",
					agent:  seOpts.agent,
					dryRun: false,
				}, result.SourcePath)
				if err != nil {
					return err
				}
				full.Mine = &mined
			}
			w := cmd.OutOrStdout()
			if opts.json {
				return writeJSON(w, "session_evidence_ingest_result", full)
			}
			lines := []string{}
			if result.Path != "" {
				lines = append(lines, result.Path)
			}
			if full.Mine != nil {
				lines = append(lines, fmt.Sprintf("mined=%d added=%d duplicates=%d", full.Mine.Chunks, full.Mine.Added, full.Mine.Duplicates))
			}
			return frontmatter(w, []kv{
				{k: "session_id", v: result.SessionID},
				{k: "selected", v: fmt.Sprintf("%d", result.Selected)},
				{k: "skipped", v: fmt.Sprintf("%d", result.Skipped)},
				{k: "source_path", v: result.SourcePath},
			}, strings.Join(lines, "\n"))
		},
	}
	addScopeFlags(c, &seOpts.scope)
	c.Flags().StringVar(&seOpts.file, "file", "-", "read transcript payload from a file, or '-' for stdin")
	c.Flags().StringVar(&seOpts.agent, "agent", "codex", "source agent name")
	c.Flags().StringVar(&seOpts.session, "session-id", "", "session ID override")
	c.Flags().IntVar(&seOpts.minChars, "min-chars", 0, "minimum chars for non-directive evidence")
	c.Flags().BoolVar(&seOpts.force, "force", false, "ingest even when session evidence is disabled")
	c.Flags().BoolVar(&seOpts.noMine, "no-mine", false, "write compact evidence without mining it into memory")
	return c
}

func newSessionEvidenceListCommand() *cobra.Command {
	var seOpts sessionEvidenceOptions
	c := &cobra.Command{
		Use:   "list",
		Short: "List compact session evidence files",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			sc, err := resolveReadScope(cmd, seOpts.scope)
			if err != nil {
				return err
			}
			stateDir, err := config.ResolveStateDir()
			if err != nil {
				return err
			}
			files, err := sessionevidence.List(stateDir, sc.ID)
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			if opts.json {
				return writeJSON(w, "session_evidence_list_result", files)
			}
			var lines []string
			for _, file := range files {
				line := fmt.Sprintf("%s\t%d\t%s", file.SessionID, len(file.Records), file.SourcePath)
				if seOpts.minimal {
					fmt.Fprintln(w, line)
				} else {
					lines = append(lines, line)
				}
			}
			if seOpts.minimal {
				return nil
			}
			return frontmatter(w, []kv{{k: "scope", v: sc.Kind}, {k: "scope_id", v: sc.ID}, {k: "count", v: fmt.Sprintf("%d", len(files))}}, strings.Join(lines, "\n"))
		},
	}
	addScopeFlags(c, &seOpts.scope)
	c.Flags().BoolVar(&seOpts.minimal, "minimal", false, "print tab-separated rows")
	return c
}

func newSessionEvidenceShowCommand() *cobra.Command {
	var seOpts sessionEvidenceOptions
	c := &cobra.Command{
		Use:   "show <session-id>",
		Short: "Show compact evidence for one session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sc, err := resolveReadScope(cmd, seOpts.scope)
			if err != nil {
				return err
			}
			stateDir, err := config.ResolveStateDir()
			if err != nil {
				return err
			}
			files, err := sessionevidence.FindBySession(stateDir, sc.ID, args[0])
			if err != nil {
				return err
			}
			if len(files) == 0 {
				return fmt.Errorf("session evidence %q not found", args[0])
			}
			w := cmd.OutOrStdout()
			if opts.json {
				return writeJSON(w, "session_evidence_show_result", files)
			}
			var b strings.Builder
			for _, file := range files {
				for _, record := range file.Records {
					fmt.Fprintf(&b, "## turn %d-%d\n", record.TurnStart, record.TurnEnd)
					fmt.Fprintf(&b, "evidence_type: %s\n", record.EvidenceType)
					fmt.Fprintf(&b, "selector_reason: %s\n", record.SelectorReason)
					fmt.Fprintf(&b, "timestamp: %s\n\n", record.Timestamp)
					fmt.Fprintf(&b, "%s\n\n", record.Content)
				}
			}
			return frontmatter(w, []kv{{k: "session_id", v: args[0]}, {k: "files", v: fmt.Sprintf("%d", len(files))}}, strings.TrimRight(b.String(), "\n"))
		},
	}
	addScopeFlags(c, &seOpts.scope)
	return c
}

func newSessionEvidenceForgetCommand() *cobra.Command {
	var seOpts sessionEvidenceOptions
	c := &cobra.Command{
		Use:   "forget <session-id>",
		Short: "Purge compact evidence and mined memories for one session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sc, err := resolveReadScope(cmd, seOpts.scope)
			if err != nil {
				return err
			}
			stateDir, err := config.ResolveStateDir()
			if err != nil {
				return err
			}
			files, err := sessionevidence.FindBySession(stateDir, sc.ID, args[0])
			if err != nil {
				return err
			}
			if len(files) == 0 {
				return fmt.Errorf("session evidence %q not found", args[0])
			}
			st, _, err := openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			ctx := context.Background()
			var ids []string
			for _, file := range files {
				result, err := st.DestroySourceMemories(ctx, sc.Kind, sc.ID, sessionevidence.SourceKind, file.SourcePath)
				if err != nil {
					return err
				}
				ids = append(ids, result.IDs...)
				if err := os.Remove(file.Path); err != nil && !os.IsNotExist(err) {
					return err
				}
			}
			result := map[string]any{"session_id": args[0], "files": len(files), "memory_ids": ids, "count": len(ids), "destroyed": true}
			w := cmd.OutOrStdout()
			if opts.json {
				return writeJSON(w, "session_evidence_forget_result", result)
			}
			return frontmatter(w, []kv{{k: "session_id", v: args[0]}, {k: "files", v: fmt.Sprintf("%d", len(files))}, {k: "destroyed", v: "true"}, {k: "count", v: fmt.Sprintf("%d", len(ids))}}, strings.Join(ids, "\n"))
		},
	}
	addScopeFlags(c, &seOpts.scope)
	return c
}

func mineSessionEvidence(ctx context.Context, st *store.Store, sc scope.Scope, mineOpts mineOptions, onlySourcePath string) (mineResult, error) {
	if !mineOpts.dryRun && st == nil {
		return mineResult{}, fmt.Errorf("store is required when mining session evidence")
	}
	stateDir, err := config.ResolveStateDir()
	if err != nil {
		return mineResult{}, err
	}
	files, err := sessionevidence.List(stateDir, sc.ID)
	if err != nil {
		return mineResult{}, err
	}
	result := mineResult{
		Root:    stateDir,
		Scope:   sc.Kind,
		ScopeID: sc.ID,
		DryRun:  mineOpts.dryRun,
	}
	sourceIDs := map[string][]string{}
	limitReached := false
	for _, file := range files {
		if limitReached {
			break
		}
		if onlySourcePath != "" && file.SourcePath != onlySourcePath {
			continue
		}
		sourceAgent := mineOpts.agent
		if len(file.Records) > 0 && strings.TrimSpace(file.Records[0].SourceAgent) != "" {
			sourceAgent = file.Records[0].SourceAgent
		}
		result.FilesScanned++
		for _, record := range file.Records {
			item := mineChunkResult{SourcePath: file.SourcePath, SourceRef: fmt.Sprintf("turn %d-%d", record.TurnStart, record.TurnEnd)}
			if mineOpts.limit > 0 && result.Chunks >= mineOpts.limit {
				limitReached = true
				break
			}
			result.Chunks++
			if !mineOpts.dryRun {
				metadata, err := sessionEvidenceMetadataJSON(file, record)
				if err != nil {
					return mineResult{}, err
				}
				createdAt := validRFC3339OrEmpty(record.Timestamp)
				mem, duplicate, err := st.AddMemory(ctx, store.AddMemoryParams{
					Role:         "source",
					Content:      record.Content,
					SourceKind:   sessionevidence.SourceKind,
					SourceAgent:  firstNonEmpty(record.SourceAgent, sourceAgent),
					SourcePath:   file.SourcePath,
					SourceRef:    item.SourceRef,
					ScopeKind:    sc.Kind,
					ScopeID:      sc.ID,
					ProjectID:    sc.ProjectID,
					SessionID:    record.SessionID,
					MetadataJSON: metadata,
					CreatedAt:    createdAt,
				})
				if err != nil {
					return mineResult{}, err
				}
				item.ID = mem.ID
				item.Duplicate = duplicate
				sourceIDs[file.SourcePath] = append(sourceIDs[file.SourcePath], mem.ID)
				if duplicate {
					result.Duplicates++
				} else {
					result.Added++
				}
			}
			result.Results = append(result.Results, item)
		}
		if !mineOpts.dryRun {
			refreshed, err := st.RefreshSource(ctx, store.SourceRefreshParams{
				Kind:            sessionevidence.SourceKind,
				Path:            file.SourcePath,
				Agent:           sourceAgent,
				ScopeKind:       sc.Kind,
				ScopeID:         sc.ID,
				Role:            "source",
				ContentHash:     file.Hash,
				ModTime:         file.ModTime,
				Size:            file.Size,
				ChunkCount:      len(file.Records),
				ActiveMemoryIDs: sourceIDs[file.SourcePath],
			})
			if err != nil {
				return mineResult{}, err
			}
			result.Sources++
			result.Staled += refreshed.Staled
		}
	}
	return result, nil
}

type sessionEvidenceMetadata struct {
	Kind           string `json:"kind"`
	SessionID      string `json:"session_id"`
	EvidenceType   string `json:"evidence_type"`
	SelectorReason string `json:"selector_reason"`
	TurnStart      int    `json:"turn_start"`
	TurnEnd        int    `json:"turn_end"`
	FileHash       string `json:"file_hash,omitempty"`
	FileMTime      string `json:"file_mtime,omitempty"`
	FileSize       int64  `json:"file_size,omitempty"`
}

func sessionEvidenceMetadataJSON(file sessionevidence.File, record sessionevidence.Record) (string, error) {
	data, err := json.Marshal(sessionEvidenceMetadata{
		Kind:           sessionevidence.SourceKind,
		SessionID:      record.SessionID,
		EvidenceType:   record.EvidenceType,
		SelectorReason: record.SelectorReason,
		TurnStart:      record.TurnStart,
		TurnEnd:        record.TurnEnd,
		FileHash:       file.Hash,
		FileMTime:      file.ModTime,
		FileSize:       file.Size,
	})
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func readSessionEvidenceInput(path string) ([]byte, error) {
	if path == "" || path == "-" {
		stat, err := os.Stdin.Stat()
		if err != nil {
			return nil, err
		}
		if stat.Mode()&os.ModeCharDevice != 0 {
			return nil, fmt.Errorf("provide --file or pipe a transcript payload on stdin")
		}
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
}

func effectiveSettings() (config.Settings, error) {
	_, settings, err := loadProjectSettings()
	return settings, err
}

func effectiveMinChars(settings config.Settings, flagValue int) int {
	if flagValue > 0 {
		return flagValue
	}
	return settings.Int("session-evidence.min-chars", sessionevidence.DefaultMinChars)
}

func validRFC3339OrEmpty(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	if _, err := time.Parse(time.RFC3339, value); err != nil {
		return ""
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
