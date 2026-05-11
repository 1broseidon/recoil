package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/1broseidon/recoil/internal/store"
	"github.com/spf13/cobra"
)

type addOptions struct {
	scope      scopeOptions
	file       string
	role       string
	agent      string
	sourcePath string
	sourceRef  string
	metadata   string
}

type addResult struct {
	Memory    *store.Memory `json:"memory"`
	Duplicate bool          `json:"duplicate"`
}

func newAddCommand() *cobra.Command {
	var addOpts addOptions
	c := &cobra.Command{
		Use:   "add [text]",
		Short: "Add a verbatim memory",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			content, err := readAddInput(addOpts.file, args)
			if err != nil {
				return err
			}
			if strings.TrimSpace(content) == "" {
				return fmt.Errorf("memory content is empty")
			}

			sc, err := resolveScope(cmd, addOpts.scope)
			if err != nil {
				return err
			}
			st, _, err := openStore()
			if err != nil {
				return err
			}
			defer st.Close()

			mem, duplicate, err := st.AddMemory(context.Background(), store.AddMemoryParams{
				Role:         addOpts.role,
				Content:      content,
				SourceAgent:  addOpts.agent,
				SourcePath:   addOpts.sourcePath,
				SourceRef:    addOpts.sourceRef,
				ScopeKind:    sc.Kind,
				ScopeID:      sc.ID,
				ProjectID:    sc.ProjectID,
				SessionID:    sc.SessionID,
				MetadataJSON: addOpts.metadata,
			})
			if err != nil {
				return err
			}

			w := cmd.OutOrStdout()
			if opts.json {
				return writeJSON(w, addResult{Memory: mem, Duplicate: duplicate})
			}
			return frontmatter(w, []kv{
				{k: "id", v: mem.ID},
				{k: "scope", v: mem.ScopeKind},
				{k: "scope_id", v: mem.ScopeID},
				{k: "created", v: mem.CreatedAt},
				{k: "duplicate", v: fmt.Sprintf("%t", duplicate)},
			}, mem.Content)
		},
	}
	addScopeFlags(c, &addOpts.scope)
	c.Flags().StringVar(&addOpts.file, "file", "", "read memory content from a file, or '-' for stdin")
	c.Flags().StringVar(&addOpts.role, "role", "", "role associated with the memory content")
	c.Flags().StringVar(&addOpts.agent, "agent", "", "source agent name")
	c.Flags().StringVar(&addOpts.sourcePath, "source-path", "", "source file or transcript path")
	c.Flags().StringVar(&addOpts.sourceRef, "source-ref", "", "source reference within the path")
	c.Flags().StringVar(&addOpts.metadata, "metadata", "", "custom metadata as JSON")
	return c
}

func readAddInput(file string, args []string) (string, error) {
	if file != "" {
		if file == "-" {
			b, err := io.ReadAll(os.Stdin)
			return string(b), err
		}
		b, err := os.ReadFile(file)
		return string(b), err
	}
	if len(args) > 0 {
		return strings.Join(args, " "), nil
	}
	stat, err := os.Stdin.Stat()
	if err != nil {
		return "", err
	}
	if stat.Mode()&os.ModeCharDevice == 0 {
		b, err := io.ReadAll(os.Stdin)
		return string(b), err
	}
	return "", fmt.Errorf("provide memory text, --file, or piped stdin")
}
