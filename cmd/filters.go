package cmd

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/1broseidon/recoil/internal/scope"
	"github.com/1broseidon/recoil/internal/store"
	"github.com/spf13/cobra"
)

type memoryFilterOptions struct {
	since  string
	before string
	agent  string
	source string
}

func addMemoryFilterFlags(c *cobra.Command, opts *memoryFilterOptions) {
	c.Flags().StringVar(&opts.since, "since", "", "filter memories created since a date or duration like 7d")
	c.Flags().StringVar(&opts.before, "before", "", "filter memories created before a date or duration like 7d")
	c.Flags().StringVar(&opts.agent, "agent", "", "filter by source agent")
	c.Flags().StringVar(&opts.source, "source", "", "filter by source path substring")
}

func searchParams(query string, sc scope.Scope, filters memoryFilterOptions, limit int) (store.SearchParams, error) {
	since, before, err := parseTimeFilters(filters)
	if err != nil {
		return store.SearchParams{}, err
	}
	return store.SearchParams{
		Query:       query,
		ScopeKind:   sc.Kind,
		ScopeID:     sc.ID,
		SourceAgent: filters.agent,
		SourcePath:  filters.source,
		Since:       since,
		Before:      before,
		Limit:       limit,
	}, nil
}

func listParams(sc scope.Scope, filters memoryFilterOptions, limit int, includeDeleted bool) (store.ListParams, error) {
	since, before, err := parseTimeFilters(filters)
	if err != nil {
		return store.ListParams{}, err
	}
	return store.ListParams{
		ScopeKind:      sc.Kind,
		ScopeID:        sc.ID,
		SourceAgent:    filters.agent,
		SourcePath:     filters.source,
		Since:          since,
		Before:         before,
		Limit:          limit,
		IncludeDeleted: includeDeleted,
	}, nil
}

func parseTimeFilters(filters memoryFilterOptions) (string, string, error) {
	since, err := parseTimeFilter(filters.since)
	if err != nil {
		return "", "", fmt.Errorf("invalid --since: %w", err)
	}
	before, err := parseTimeFilter(filters.before)
	if err != nil {
		return "", "", fmt.Errorf("invalid --before: %w", err)
	}
	return since, before, nil
}

func parseTimeFilter(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if strings.HasSuffix(value, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(value, "d"))
		if err != nil || days < 0 {
			return "", fmt.Errorf("expected positive day duration, got %q", value)
		}
		return time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour).Format(time.RFC3339), nil
	}
	if dur, err := time.ParseDuration(value); err == nil {
		return time.Now().UTC().Add(-dur).Format(time.RFC3339), nil
	}
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t.UTC().Format(time.RFC3339), nil
	}
	if t, err := time.Parse("2006-01-02", value); err == nil {
		return t.UTC().Format(time.RFC3339), nil
	}
	return "", fmt.Errorf("expected RFC3339, YYYY-MM-DD, or duration")
}
