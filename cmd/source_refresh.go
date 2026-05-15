package cmd

import (
	"context"
	"os"
	"path/filepath"

	"github.com/1broseidon/recoil/internal/mine"
	"github.com/1broseidon/recoil/internal/scope"
	"github.com/1broseidon/recoil/internal/store"
)

type sourceRefreshSummary struct {
	Checked          int `json:"checked"`
	RefreshedSources int `json:"refreshed_sources"`
	StaledMemories   int `json:"staled_memories"`
	DeletedSources   int `json:"deleted_sources"`
}

func refreshTrackedProjectSources(ctx context.Context, st *store.Store, sc scope.Scope) (sourceRefreshSummary, error) {
	var summary sourceRefreshSummary
	if sc.Kind != "project" || sc.Root == "" {
		return summary, nil
	}
	sources, err := st.FileSources(ctx, sc.Kind, sc.ID, "recoil")
	if err != nil {
		return summary, err
	}
	if len(sources) == 0 {
		return summary, nil
	}
	summary.Checked = len(sources)
	currentPaths := make([]string, 0, len(sources))
	for _, src := range sources {
		rel := filepath.ToSlash(src.Path)
		full := filepath.Join(sc.Root, filepath.FromSlash(rel))
		info, err := os.Stat(full)
		if err != nil {
			if os.IsNotExist(err) {
				summary.DeletedSources++
				continue
			}
			return summary, err
		}
		if info.IsDir() {
			summary.DeletedSources++
			continue
		}
		file := mine.File{
			Path:    full,
			Rel:     rel,
			Size:    info.Size(),
			ModTime: info.ModTime().UTC().Format("2006-01-02T15:04:05Z07:00"),
		}
		chunks, err := mine.ChunksForFile(file, mine.Options{})
		if err != nil {
			summary.DeletedSources++
			continue
		}
		if len(chunks) == 0 {
			summary.DeletedSources++
			continue
		}
		currentPaths = append(currentPaths, rel)
		if chunks[0].FileHash == src.ContentHash {
			continue
		}
		sourceIDs := make([]string, 0, len(chunks))
		for _, chunk := range chunks {
			metadata, err := mineMetadataJSON(chunk)
			if err != nil {
				return summary, err
			}
			mem, _, err := st.AddMemory(ctx, store.AddMemoryParams{
				Role:         "source",
				Content:      chunk.Content,
				SourceKind:   "file",
				SourceAgent:  "recoil",
				SourcePath:   chunk.SourcePath,
				SourceRef:    chunk.SourceRef,
				ScopeKind:    sc.Kind,
				ScopeID:      sc.ID,
				ProjectID:    sc.ProjectID,
				SessionID:    sc.SessionID,
				MetadataJSON: metadata,
			})
			if err != nil {
				return summary, err
			}
			sourceIDs = append(sourceIDs, mem.ID)
		}
		refreshed, err := st.RefreshSource(ctx, store.SourceRefreshParams{
			Kind:            "file",
			Path:            rel,
			Agent:           "recoil",
			ScopeKind:       sc.Kind,
			ScopeID:         sc.ID,
			Role:            "source",
			ContentHash:     chunks[0].FileHash,
			ModTime:         chunks[0].FileMTime,
			Size:            chunks[0].FileSize,
			ChunkCount:      len(chunks),
			ActiveMemoryIDs: sourceIDs,
		})
		if err != nil {
			return summary, err
		}
		if refreshed.Changed {
			summary.RefreshedSources++
		}
		summary.StaledMemories += refreshed.Staled
	}
	staled, err := st.StaleMissingSources(ctx, sc.Kind, sc.ID, "recoil", currentPaths)
	if err != nil {
		return summary, err
	}
	summary.StaledMemories += staled
	return summary, nil
}
