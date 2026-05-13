package cmd

import (
	"sort"
	"strconv"
	"strings"

	"github.com/1broseidon/recoil/internal/config"
	"github.com/1broseidon/recoil/internal/sourcequality"
)

type minePolicy struct {
	IncludeHiddenOperational bool
	FollowRepoSymlinks       bool
	IncludePaths             []string
	ExcludePaths             []string
}

func effectiveMinePolicy(settings config.Settings) minePolicy {
	return minePolicy{
		IncludeHiddenOperational: settings.Bool("mine.include_hidden_operational", true),
		FollowRepoSymlinks:       settings.Bool("mine.follow_repo_symlinks", true),
		IncludePaths:             settings.PathList("mine.include_paths"),
		ExcludePaths:             settings.PathList("mine.exclude_paths"),
	}
}

func effectiveSourceQualityOptions(settings config.Settings) sourcequality.Options {
	opts := sourcequality.Options{
		SearchBoosts:    map[string]float64{},
		SearchPenalties: map[string]float64{},
		WakeBoosts:      map[string]float64{},
		WakePenalties:   map[string]float64{},
	}
	keys := make([]string, 0, len(settings.Values))
	for key := range settings.Values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := strings.TrimSpace(settings.Values[key])
		switch {
		case strings.HasPrefix(key, "classify.override."):
			pattern := strings.TrimSpace(strings.TrimPrefix(key, "classify.override."))
			if pattern != "" && value != "" {
				opts.ClassOverrides = append(opts.ClassOverrides, sourcequality.ClassOverride{
					Pattern:  pattern,
					DocClass: value,
				})
			}
		case strings.HasPrefix(key, "rank.search.boost."):
			setRankAdjustment(opts.SearchBoosts, key, "rank.search.boost.", value)
		case strings.HasPrefix(key, "rank.search.penalty."):
			setRankAdjustment(opts.SearchPenalties, key, "rank.search.penalty.", value)
		case strings.HasPrefix(key, "rank.wake.boost."):
			setRankAdjustment(opts.WakeBoosts, key, "rank.wake.boost.", value)
		case strings.HasPrefix(key, "rank.wake.penalty."):
			setRankAdjustment(opts.WakePenalties, key, "rank.wake.penalty.", value)
		}
	}
	sort.SliceStable(opts.ClassOverrides, func(i, j int) bool {
		return len(opts.ClassOverrides[i].Pattern) > len(opts.ClassOverrides[j].Pattern)
	})
	return opts
}

func setRankAdjustment(target map[string]float64, key, prefix, value string) {
	docClass := strings.TrimSpace(strings.TrimPrefix(key, prefix))
	if docClass == "" {
		return
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return
	}
	target[docClass] = parsed
}
