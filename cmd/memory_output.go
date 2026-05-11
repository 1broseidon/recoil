package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/1broseidon/recoil/internal/store"
)

func writeMinimalMemory(w io.Writer, mem store.Memory, includeScore bool) {
	excerpt := mem.Excerpt
	if excerpt == "" {
		excerpt = mem.Content
	}
	excerpt = truncateText(oneLine(excerpt), 180)
	source := mem.SourceAgent
	if source == "" {
		source = mem.SourcePath
	}
	if includeScore {
		fmt.Fprintf(w, "%s\t%.4f\t%s\t%s\t%s\n", mem.ID, mem.Score, mem.CreatedAt, source, excerpt)
		return
	}
	fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", mem.ID, mem.CreatedAt, source, excerpt)
}

func memoryBlocks(memories []store.Memory, maxChars int, includeScore bool) string {
	var b strings.Builder
	remaining := maxChars
	for _, mem := range memories {
		fmt.Fprintf(&b, "## %s\n", mem.ID)
		if includeScore {
			fmt.Fprintf(&b, "score: %.4f\n", mem.Score)
		}
		fmt.Fprintf(&b, "created: %s\n", mem.CreatedAt)
		if mem.Validity != "" {
			fmt.Fprintf(&b, "validity: %s\n", mem.Validity)
		}
		if mem.ClaimKey != "" {
			fmt.Fprintf(&b, "claim_key: %s\n", mem.ClaimKey)
		}
		if mem.Supersedes != "" {
			fmt.Fprintf(&b, "supersedes: %s\n", mem.Supersedes)
		}
		if mem.SupersededBy != "" {
			fmt.Fprintf(&b, "superseded_by: %s\n", mem.SupersededBy)
		}
		if mem.Role != "" {
			fmt.Fprintf(&b, "role: %s\n", mem.Role)
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
		body := mem.Content
		if remaining > 0 {
			body = truncateText(body, remaining)
			remaining -= len(body)
		}
		fmt.Fprintf(&b, "\n%s\n\n", body)
		if maxChars > 0 && remaining <= 0 {
			break
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
