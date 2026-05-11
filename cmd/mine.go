package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/1broseidon/recoil/internal/mine"
	"github.com/1broseidon/recoil/internal/store"
	"github.com/spf13/cobra"
)

type mineOptions struct {
	scope         scopeOptions
	dryRun        bool
	includeHidden bool
	limit         int
	maxFileBytes  int64
	maxChars      int
	role          string
	agent         string
}

type mineResult struct {
	Root         string            `json:"root"`
	Scope        string            `json:"scope"`
	ScopeID      string            `json:"scope_id"`
	DryRun       bool              `json:"dry_run"`
	FilesScanned int               `json:"files_scanned"`
	FilesSkipped int               `json:"files_skipped"`
	Chunks       int               `json:"chunks"`
	Added        int               `json:"added"`
	Duplicates   int               `json:"duplicates"`
	Sources      int               `json:"sources"`
	Staled       int               `json:"staled"`
	Results      []mineChunkResult `json:"results,omitempty"`
	Skipped      []mine.Skip       `json:"skipped,omitempty"`
}

type mineChunkResult struct {
	ID         string `json:"id,omitempty"`
	SourcePath string `json:"source_path"`
	SourceRef  string `json:"source_ref"`
	Duplicate  bool   `json:"duplicate,omitempty"`
}

type minedMetadata struct {
	Kind       string `json:"kind"`
	ChunkIndex int    `json:"chunk_index"`
	StartLine  int    `json:"start_line"`
	EndLine    int    `json:"end_line"`
	FileHash   string `json:"file_hash,omitempty"`
	FileMTime  string `json:"file_mtime,omitempty"`
	FileSize   int64  `json:"file_size,omitempty"`
}

func newMineCommand() *cobra.Command {
	var mineOpts mineOptions
	c := &cobra.Command{
		Use:   "mine [path]",
		Short: "Import conservative project file memories",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sc, err := resolveScope(cmd, mineOpts.scope)
			if err != nil {
				return err
			}
			path := "."
			if len(args) == 1 {
				path = args[0]
			} else if sc.Kind == "project" && sc.Root != "" {
				path = sc.Root
			}

			collected, err := mine.Collect(mine.Options{
				Path:          path,
				SourceRoot:    sc.Root,
				IncludeHidden: mineOpts.includeHidden,
				MaxFileBytes:  mineOpts.maxFileBytes,
				MaxChunkChars: mineOpts.maxChars,
				MaxChunks:     mineOpts.limit,
			})
			if err != nil {
				return err
			}

			result := mineResult{
				Root:         collected.Root,
				Scope:        sc.Kind,
				ScopeID:      sc.ID,
				DryRun:       mineOpts.dryRun,
				FilesScanned: collected.FilesScanned,
				FilesSkipped: collected.FilesSkipped,
				Chunks:       len(collected.Chunks),
				Skipped:      collected.Skipped,
			}

			ctx := context.Background()
			var st *store.Store
			if !mineOpts.dryRun {
				var err error
				st, _, err = openStore()
				if err != nil {
					return err
				}
				defer st.Close()
			}

			sourceIDs := map[string][]string{}
			sourceChunks := map[string][]mine.Chunk{}
			for _, chunk := range collected.Chunks {
				item := mineChunkResult{
					SourcePath: chunk.SourcePath,
					SourceRef:  chunk.SourceRef,
				}
				if !mineOpts.dryRun {
					metadata, err := mineMetadataJSON(chunk)
					if err != nil {
						return err
					}
					mem, duplicate, err := st.AddMemory(ctx, store.AddMemoryParams{
						Role:         mineOpts.role,
						Content:      chunk.Content,
						SourceAgent:  mineOpts.agent,
						SourcePath:   chunk.SourcePath,
						SourceRef:    chunk.SourceRef,
						ScopeKind:    sc.Kind,
						ScopeID:      sc.ID,
						ProjectID:    sc.ProjectID,
						SessionID:    sc.SessionID,
						MetadataJSON: metadata,
					})
					if err != nil {
						return err
					}
					item.ID = mem.ID
					item.Duplicate = duplicate
					sourceIDs[chunk.SourcePath] = append(sourceIDs[chunk.SourcePath], mem.ID)
					sourceChunks[chunk.SourcePath] = append(sourceChunks[chunk.SourcePath], chunk)
					if duplicate {
						result.Duplicates++
					} else {
						result.Added++
					}
				}
				result.Results = append(result.Results, item)
			}
			if !mineOpts.dryRun {
				for sourcePath, chunks := range sourceChunks {
					if len(chunks) == 0 {
						continue
					}
					first := chunks[0]
					refreshed, err := st.RefreshSource(ctx, store.SourceRefreshParams{
						Kind:            "file",
						Path:            sourcePath,
						Agent:           mineOpts.agent,
						ScopeKind:       sc.Kind,
						ScopeID:         sc.ID,
						Role:            mineOpts.role,
						ContentHash:     first.FileHash,
						ModTime:         first.FileMTime,
						Size:            first.FileSize,
						ChunkCount:      len(chunks),
						ActiveMemoryIDs: sourceIDs[sourcePath],
					})
					if err != nil {
						return err
					}
					result.Sources++
					result.Staled += refreshed.Staled
				}
				if sc.Kind == "project" && sc.Root != "" && collected.Root == sc.Root {
					paths := make([]string, 0, len(sourceChunks))
					for path := range sourceChunks {
						paths = append(paths, path)
					}
					staled, err := st.StaleMissingSources(ctx, sc.Kind, sc.ID, mineOpts.agent, paths)
					if err != nil {
						return err
					}
					result.Staled += staled
				}
			}

			w := cmd.OutOrStdout()
			if opts.json {
				return writeJSON(w, "mine_result", result)
			}
			return frontmatter(w, []kv{
				{k: "root", v: result.Root},
				{k: "scope", v: result.Scope},
				{k: "scope_id", v: result.ScopeID},
				{k: "dry_run", v: fmt.Sprintf("%t", result.DryRun)},
				{k: "files_scanned", v: fmt.Sprintf("%d", result.FilesScanned)},
				{k: "files_skipped", v: fmt.Sprintf("%d", result.FilesSkipped)},
				{k: "chunks", v: fmt.Sprintf("%d", result.Chunks)},
				{k: "added", v: fmt.Sprintf("%d", result.Added)},
				{k: "duplicates", v: fmt.Sprintf("%d", result.Duplicates)},
				{k: "sources", v: fmt.Sprintf("%d", result.Sources)},
				{k: "staled", v: fmt.Sprintf("%d", result.Staled)},
			}, mineResultLines(result))
		},
	}
	addScopeFlags(c, &mineOpts.scope)
	c.Flags().BoolVar(&mineOpts.dryRun, "dry-run", false, "preview mined chunks without writing memories")
	c.Flags().BoolVar(&mineOpts.includeHidden, "include-hidden", false, "include hidden files except protected tooling directories")
	c.Flags().IntVar(&mineOpts.limit, "limit", 0, "maximum chunks to mine; 0 means no limit")
	c.Flags().Int64Var(&mineOpts.maxFileBytes, "max-file-bytes", mine.DefaultMaxFileBytes, "maximum bytes per file")
	c.Flags().IntVar(&mineOpts.maxChars, "max-chars", mine.DefaultMaxChunkChars, "maximum characters per mined chunk")
	c.Flags().StringVar(&mineOpts.role, "role", "source", "role assigned to mined memories")
	c.Flags().StringVar(&mineOpts.agent, "agent", "recoil", "source agent assigned to mined memories")
	return c
}

func mineMetadataJSON(chunk mine.Chunk) (string, error) {
	data, err := json.Marshal(minedMetadata{
		Kind:       "file_chunk",
		ChunkIndex: chunk.Index,
		StartLine:  chunk.StartLine,
		EndLine:    chunk.EndLine,
		FileHash:   chunk.FileHash,
		FileMTime:  chunk.FileMTime,
		FileSize:   chunk.FileSize,
	})
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func mineResultLines(result mineResult) string {
	lines := make([]string, 0, len(result.Results))
	for _, item := range result.Results {
		id := item.ID
		if id == "" {
			id = "dry-run"
		}
		status := "added"
		if item.Duplicate {
			status = "duplicate"
		}
		if result.DryRun {
			status = "would-add"
		}
		lines = append(lines, fmt.Sprintf("%s\t%s\t%s\t%s", id, status, item.SourcePath, item.SourceRef))
	}
	return strings.Join(lines, "\n")
}
