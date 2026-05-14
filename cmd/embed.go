package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/1broseidon/recoil/internal/embedding"
	"github.com/1broseidon/recoil/internal/store"
	"github.com/spf13/cobra"
)

type embedOptions struct {
	provider string
	model    string
}

type embedIndexOptions struct {
	scope   scopeOptions
	filters memoryFilterOptions
	limit   int
}

type embedSearchOptions struct {
	scope    scopeOptions
	filters  memoryFilterOptions
	limit    int
	minimal  bool
	maxChars int
}

type embedIndexResult struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Scope    string `json:"scope"`
	ScopeID  string `json:"scope_id"`
	Indexed  int    `json:"indexed"`
}

func newEmbedCommand() *cobra.Command {
	var opts embedOptions
	var indexOpts embedIndexOptions
	var searchOpts embedSearchOptions
	c := &cobra.Command{
		Use:   "embed",
		Short: "Index and search optional embedding sidecars",
	}
	c.PersistentFlags().StringVar(&opts.provider, "provider", embedding.DefaultLocalProvider, "embedding provider")
	c.PersistentFlags().StringVar(&opts.model, "model", embedding.DefaultLocalModel, "embedding model")
	c.AddCommand(newEmbedIndexCommand(&opts, &indexOpts))
	c.AddCommand(newEmbedSearchCommand(&opts, &searchOpts))
	return c
}

func newEmbedIndexCommand(embedOpts *embedOptions, indexOpts *embedIndexOptions) *cobra.Command {
	c := &cobra.Command{
		Use:   "index",
		Short: "Build embedding sidecars for scoped memories",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			sc, err := resolveReadScope(cmd, indexOpts.scope)
			if err != nil {
				return err
			}
			provider, err := newEmbeddingProvider(embedOpts.provider, embedOpts.model)
			if err != nil {
				return err
			}
			params, err := listParams(sc, indexOpts.filters, indexOpts.limit, false)
			if err != nil {
				return err
			}
			if params.Lifecycle == store.LifecycleAny && params.Validity == "" {
				params.Lifecycle = store.LifecycleCurrent
			}
			st, _, err := openStore()
			if err != nil {
				return err
			}
			defer st.Close()

			indexed, err := indexEmbeddings(context.Background(), st, provider, params)
			if err != nil {
				return err
			}
			result := embedIndexResult{
				Provider: provider.Name(),
				Model:    provider.Model(),
				Scope:    sc.Kind,
				ScopeID:  sc.ID,
				Indexed:  indexed,
			}
			w := cmd.OutOrStdout()
			if opts.json {
				return writeJSON(w, "embed_index_result", result)
			}
			return frontmatter(w, []kv{
				{k: "provider", v: result.Provider},
				{k: "model", v: result.Model},
				{k: "scope", v: result.Scope},
				{k: "scope_id", v: result.ScopeID},
				{k: "indexed", v: fmt.Sprintf("%d", result.Indexed)},
			}, "")
		},
	}
	addScopeFlags(c, &indexOpts.scope)
	addMemoryFilterFlags(c, &indexOpts.filters)
	c.Flags().IntVar(&indexOpts.limit, "limit", 1000, "maximum number of memories to index")
	return c
}

func newEmbedSearchCommand(embedOpts *embedOptions, searchOpts *embedSearchOptions) *cobra.Command {
	c := &cobra.Command{
		Use:   "search [query]",
		Short: "Search indexed embedding sidecars",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := strings.TrimSpace(strings.Join(args, " "))
			if query == "" {
				return fmt.Errorf("query is empty")
			}
			sc, err := resolveReadScope(cmd, searchOpts.scope)
			if err != nil {
				return err
			}
			provider, err := newEmbeddingProvider(embedOpts.provider, embedOpts.model)
			if err != nil {
				return err
			}
			params, err := searchParams(query, sc, searchOpts.filters, searchOpts.limit)
			if err != nil {
				return err
			}
			if params.Lifecycle == store.LifecycleAny && params.Validity == "" {
				params.Lifecycle = store.LifecycleCurrent
			}
			vector, err := provider.Embed(context.Background(), query)
			if err != nil {
				return err
			}
			st, _, err := openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			results, err := st.SemanticSearch(context.Background(), semanticParamsFromSearch(params, vector, provider))
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			if opts.json {
				return writeJSON(w, "embed_search_result", results)
			}
			if searchOpts.minimal {
				for _, r := range results {
					writeMinimalMemory(w, r, true)
				}
				return nil
			}
			return frontmatter(w, []kv{
				{k: "query", v: query},
				{k: "provider", v: provider.Name()},
				{k: "model", v: provider.Model()},
				{k: "scope", v: sc.Kind},
				{k: "scope_id", v: sc.ID},
				{k: "result_count", v: fmt.Sprintf("%d", len(results))},
			}, memoryBlocks(results, searchOpts.maxChars, true))
		},
	}
	addScopeFlags(c, &searchOpts.scope)
	addMemoryFilterFlags(c, &searchOpts.filters)
	c.Flags().IntVar(&searchOpts.limit, "limit", 5, "maximum number of memories to return")
	c.Flags().BoolVar(&searchOpts.minimal, "minimal", false, "print tab-separated rows")
	c.Flags().IntVar(&searchOpts.maxChars, "max-chars", 4000, "maximum characters of memory content to print")
	return c
}

func indexEmbeddings(ctx context.Context, st *store.Store, provider embedding.Provider, params store.ListParams) (int, error) {
	memories, err := st.List(ctx, params)
	if err != nil {
		return 0, err
	}
	indexed := 0
	for _, mem := range memories {
		vector, err := provider.Embed(ctx, store.EmbeddingText(mem))
		if err != nil {
			return indexed, err
		}
		if err := st.UpsertEmbedding(ctx, mem, provider.Name(), provider.Model(), vector); err != nil {
			return indexed, err
		}
		indexed++
	}
	return indexed, nil
}

func semanticParamsFromSearch(params store.SearchParams, vector []float64, provider embedding.Provider) store.SemanticSearchParams {
	return store.SemanticSearchParams{
		QueryVector: vector,
		Provider:    provider.Name(),
		Model:       provider.Model(),
		ScopeKind:   params.ScopeKind,
		ScopeID:     params.ScopeID,
		SourceKind:  params.SourceKind,
		SourceAgent: params.SourceAgent,
		SourcePath:  params.SourcePath,
		Role:        params.Role,
		ClaimKey:    params.ClaimKey,
		Validity:    params.Validity,
		Since:       params.Since,
		Before:      params.Before,
		Limit:       params.Limit,
		Lifecycle:   params.Lifecycle,
	}
}
