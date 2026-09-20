package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/1broseidon/recoil/internal/scope"
	"github.com/1broseidon/recoil/internal/store"
	"github.com/spf13/cobra"
)

type checkOptions struct {
	scope          scopeOptions
	claimKey       string
	claimKeyPrefix string
	limit          int
}

type checkResult struct {
	Query            string                 `json:"query,omitempty"`
	ClaimKey         string                 `json:"claim_key,omitempty"`
	Verdict          string                 `json:"verdict"`
	PredicateStatus  string                 `json:"predicate_status"`
	Recommendation   string                 `json:"recommendation"`
	Reason           string                 `json:"reason"`
	RecheckPrompt    string                 `json:"recheck_prompt,omitempty"`
	DecisionStance   string                 `json:"decision_stance,omitempty"`
	DecisionSubject  string                 `json:"decision_subject,omitempty"`
	RequestedAction  string                 `json:"requested_action,omitempty"`
	Advisory         string                 `json:"advisory,omitempty"`
	MatchedMemory    *store.Memory          `json:"matched_memory,omitempty"`
	CurrentDecision  *store.Memory          `json:"current_decision,omitempty"`
	Replacement      *store.Memory          `json:"replacement,omitempty"`
	Predicate        *decisionPredicate     `json:"predicate,omitempty"`
	PredicateDetails predicateEvaluation    `json:"predicate_details,omitempty"`
	Family           []store.Memory         `json:"family,omitempty"`
	ChannelFreshness channelFreshnessResult `json:"channel_freshness"`
}

// checkPrefixResult aggregates one per-family verdict for every claim family
// under a prefix. Verdict logic stays exactly per exact key: each family is
// audited by the same runDecisionCheck the --claim-key path uses, and the
// top-level verdict is the worst family verdict (see checkVerdictSeverity), so
// an agent can gate on one field without losing the per-family detail.
type checkPrefixResult struct {
	ClaimKeyPrefix   string                 `json:"claim_key_prefix"`
	Verdict          string                 `json:"verdict"`
	Recommendation   string                 `json:"recommendation"`
	Reason           string                 `json:"reason"`
	DecidingClaimKey string                 `json:"deciding_claim_key,omitempty"`
	FamilyCount      int                    `json:"family_count"`
	Families         []checkResult          `json:"families,omitempty"`
	ChannelFreshness channelFreshnessResult `json:"channel_freshness"`
}

type decisionTrailItem struct {
	ClaimKey        string        `json:"claim_key"`
	Validity        string        `json:"validity"`
	PredicateStatus string        `json:"predicate_status"`
	Decision        *store.Memory `json:"decision,omitempty"`
	RecheckPrompt   string        `json:"recheck_prompt,omitempty"`
	Reason          string        `json:"reason,omitempty"`
}

func newCheckCommand() *cobra.Command {
	var checkOpts checkOptions
	c := &cobra.Command{
		Use:   "check [query-or-memory-id]",
		Short: "Audit whether a remembered decision is still safe to act on",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			query := strings.TrimSpace(strings.Join(args, " "))
			claimKey := strings.TrimSpace(checkOpts.claimKey)
			claimKeyPrefix := strings.TrimSpace(checkOpts.claimKeyPrefix)
			if err := validateCheckTarget(query, claimKey, claimKeyPrefix); err != nil {
				return err
			}
			if checkOpts.limit <= 0 {
				checkOpts.limit = 8
			}
			sc, err := resolveReadScope(cmd, checkOpts.scope)
			if err != nil {
				return err
			}
			// check is read-only apart from channel JIT refresh, which
			// openContextStore reopens writable for only when it can fire.
			st, _, err := openContextStore("check")
			if err != nil {
				return err
			}
			defer st.Close()
			ctx := context.Background()
			w := cmd.OutOrStdout()
			if claimKeyPrefix != "" {
				prefixResult, err := runCheckPrefix(ctx, st, sc, claimKeyPrefix, checkOpts.limit)
				if err != nil {
					return err
				}
				if opts.json {
					return writeJSON(w, "check_prefix_result", prefixResult)
				}
				return frontmatter(w, checkPrefixFrontmatter(prefixResult), renderCheckPrefixResult(prefixResult))
			}
			result, err := runCheck(ctx, st, sc, query, claimKey, checkOpts.limit)
			if err != nil {
				return err
			}
			if opts.json {
				return writeJSON(w, "check_result", result)
			}
			return frontmatter(w, checkFrontmatter(result), renderCheckResult(result))
		},
	}
	addScopeFlags(c, &checkOpts.scope)
	c.Flags().StringVar(&checkOpts.claimKey, "claim-key", "", "audit an exact claim family")
	c.Flags().StringVar(&checkOpts.claimKeyPrefix, "claim-key-prefix", "", "audit every claim family under a prefix, e.g. voice. (mutually exclusive with --claim-key and a query)")
	c.Flags().IntVar(&checkOpts.limit, "limit", 8, "maximum memories to inspect")
	return c
}

// validateCheckTarget enforces exactly one targeting mode. A prefix selects
// which families to audit, so pairing it with a free-text query (which exists
// to *find* a family) or with an exact key is contradictory.
func validateCheckTarget(query, claimKey, claimKeyPrefix string) error {
	if claimKey != "" && claimKeyPrefix != "" {
		return fmt.Errorf("choose --claim-key or --claim-key-prefix, not both")
	}
	if query != "" && claimKeyPrefix != "" {
		return fmt.Errorf("choose a query or --claim-key-prefix, not both")
	}
	if query == "" && claimKey == "" && claimKeyPrefix == "" {
		return fmt.Errorf("provide a query, memory id, --claim-key, or --claim-key-prefix")
	}
	return nil
}

func runCheck(ctx context.Context, st *store.Store, sc scope.Scope, query, claimKey string, limit int) (checkResult, error) {
	freshness := ensureFreshContext(ctx, st, "check")
	result, err := runDecisionCheck(ctx, st, sc, query, claimKey, limit)
	if err != nil {
		return checkResult{}, err
	}
	result.ChannelFreshness = freshness
	return result, nil
}

// runCheckPrefix audits every claim family whose key starts with prefix. The
// family list comes from the claim index (ordered claim_key ASC), so output
// order is stable; channel freshness is resolved once for the whole run rather
// than once per family.
func runCheckPrefix(ctx context.Context, st *store.Store, sc scope.Scope, prefix string, limit int) (checkPrefixResult, error) {
	freshness := ensureFreshContext(ctx, st, "check")
	summaries, err := st.ClaimSummaries(ctx, store.ClaimSummaryParams{
		ScopeKind:      sc.Kind,
		ScopeID:        sc.ID,
		ClaimKeyPrefix: prefix,
	})
	if err != nil {
		return checkPrefixResult{}, err
	}
	result := checkPrefixResult{
		ClaimKeyPrefix:   prefix,
		Verdict:          "no_decision",
		Recommendation:   "proceed_without_memory",
		Reason:           "no_claim_key_match",
		FamilyCount:      len(summaries),
		ChannelFreshness: freshness,
	}
	for _, summary := range summaries {
		family, err := runDecisionCheck(ctx, st, sc, "", summary.ClaimKey, limit)
		if err != nil {
			return checkPrefixResult{}, err
		}
		result.Families = append(result.Families, family)
	}
	if worst, ok := worstCheckResult(result.Families); ok {
		result.Verdict = worst.Verdict
		result.Recommendation = worst.Recommendation
		result.Reason = worst.Reason
		result.DecidingClaimKey = worst.ClaimKey
	}
	return result, nil
}

// checkVerdictSeverity ranks verdicts worst-first so an aggregate over a family
// tree cannot under-report a blocking decision.
func checkVerdictSeverity(verdict string) int {
	switch verdict {
	case "review":
		return 0
	case "use_replacement":
		return 1
	case "ignore":
		return 2
	case "use":
		return 3
	default:
		return 4
	}
}

func worstCheckResult(results []checkResult) (checkResult, bool) {
	var worst checkResult
	found := false
	for _, result := range results {
		if !found || checkVerdictSeverity(result.Verdict) < checkVerdictSeverity(worst.Verdict) {
			worst = result
			found = true
		}
	}
	return worst, found
}

func checkPrefixFrontmatter(result checkPrefixResult) []kv {
	meta := []kv{
		{k: "claim_key_prefix", v: result.ClaimKeyPrefix},
		{k: "verdict", v: result.Verdict},
		{k: "recommendation", v: result.Recommendation},
		{k: "reason", v: result.Reason},
		{k: "deciding_claim_key", v: result.DecidingClaimKey},
		{k: "family_count", v: fmt.Sprintf("%d", result.FamilyCount)},
	}
	meta = append(meta,
		kv{k: "channel_imported", v: fmt.Sprintf("%d", channelFreshnessImported(result.ChannelFreshness))},
		kv{k: "channel_errors", v: fmt.Sprintf("%d", channelFreshnessErrors(result.ChannelFreshness))},
	)
	meta = append(meta, channelFriendlyFrontmatter(result.ChannelFreshness)...)
	meta = append(meta, channelOutboxFrontmatter("channel_", result.ChannelFreshness.Outbox)...)
	return meta
}

func renderCheckPrefixResult(result checkPrefixResult) string {
	if len(result.Families) == 0 {
		return "No decision families found under prefix " + result.ClaimKeyPrefix + ".\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "## Decision Check - %s\n\n", result.ClaimKeyPrefix)
	fmt.Fprintf(&b, "verdict: %s\n", result.Verdict)
	fmt.Fprintf(&b, "recommendation: %s\n", result.Recommendation)
	fmt.Fprintf(&b, "reason: %s\n", result.Reason)
	if result.DecidingClaimKey != "" {
		fmt.Fprintf(&b, "deciding_claim_key: %s\n", result.DecidingClaimKey)
	}
	fmt.Fprintf(&b, "family_count: %d\n", result.FamilyCount)
	for _, family := range result.Families {
		fmt.Fprintf(&b, "\n%s", renderCheckResult(family))
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

func runDecisionCheck(ctx context.Context, st *store.Store, sc scope.Scope, query, claimKey string, limit int) (checkResult, error) {
	query = strings.TrimSpace(query)
	claimKey = strings.TrimSpace(claimKey)
	var matched *store.Memory
	targetKind := "query"
	if claimKey == "" && query != "" && !strings.Contains(query, " ") {
		if mem, err := st.GetMemory(ctx, query); err == nil {
			matched = mem
			claimKey = mem.ClaimKey
			targetKind = "memory"
		}
	}
	if claimKey == "" {
		found, err := searchDecisionCandidates(ctx, st, sc, query, limit)
		if err != nil {
			return checkResult{}, err
		}
		for _, mem := range found {
			if !isDecisionLikeMemory(mem) || strings.TrimSpace(mem.ClaimKey) == "" {
				continue
			}
			candidate := mem
			matched = &candidate
			claimKey = mem.ClaimKey
			break
		}
	}
	if claimKey == "" {
		return checkResult{
			Query:           query,
			Verdict:         "no_decision",
			PredicateStatus: "unknown",
			Recommendation:  "proceed_without_memory",
			Reason:          "no_claim_key_match",
		}, nil
	}
	family, err := decisionFamily(ctx, st, sc, claimKey, max(limit*4, 20))
	if err != nil {
		return checkResult{}, err
	}
	result := assessDecisionFamily(ctx, st, sc, query, claimKey, targetKind, matched, family)
	return result, nil
}

func searchDecisionCandidates(ctx context.Context, st *store.Store, sc scope.Scope, query string, limit int) ([]store.Memory, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	_, settings, _ := loadProjectSettings()
	params := store.SearchParams{
		Query:         query,
		ScopeKind:     sc.Kind,
		ScopeID:       sc.ID,
		Limit:         max(limit, 8),
		Lifecycle:     store.LifecycleAny,
		SignalRerank:  true,
		SourceQuality: effectiveSourceQualityOptions(settings),
	}
	return runRetriever(ctx, st, params, retrieverOptions{
		mode:  retrievalFTS,
		limit: params.Limit,
	})
}

func decisionFamily(ctx context.Context, st *store.Store, sc scope.Scope, claimKey string, limit int) ([]store.Memory, error) {
	return st.List(ctx, store.ListParams{
		ScopeKind: sc.Kind,
		ScopeID:   sc.ID,
		ClaimKey:  claimKey,
		Limit:     limit,
		Lifecycle: store.LifecycleAny,
	})
}

func assessDecisionFamily(ctx context.Context, st *store.Store, sc scope.Scope, query, claimKey, targetKind string, matched *store.Memory, family []store.Memory) checkResult {
	current := currentDecision(family)
	var predicateSource *store.Memory
	if current != nil {
		predicateSource = current
	} else if matched != nil {
		predicateSource = matched
	} else if len(family) > 0 {
		predicateSource = &family[0]
	}
	predicateEval := predicateEvaluation{Status: "unknown"}
	var pred *decisionPredicate
	if predicateSource != nil {
		predicateEval = evaluateDecisionPredicate(ctx, st, sc, *predicateSource)
		if parsed, ok := predicateFromMemory(*predicateSource); ok {
			pred = &parsed
		}
	}
	result := checkResult{
		Query:            query,
		ClaimKey:         claimKey,
		Verdict:          "no_decision",
		PredicateStatus:  predicateEval.Status,
		Recommendation:   "proceed_without_memory",
		Reason:           "empty_family",
		Predicate:        pred,
		PredicateDetails: predicateEval,
		Family:           family,
	}
	if matched != nil {
		result.MatchedMemory = matched
	}
	if current != nil {
		result.CurrentDecision = current
	}
	result.RecheckPrompt = firstNonEmpty(predicateEval.Prompt, predicateSourceRecheck(predicateSource))
	if predicateEval.Status == "holds" && pred != nil && strings.TrimSpace(pred.HoldsWhile) != "" {
		result.Advisory = strings.TrimSpace(pred.HoldsWhile)
	}

	currentCount := currentDecisionCount(family)
	if currentCount > 1 {
		result.Verdict = "review"
		result.Recommendation = "ask_operator"
		result.Reason = "multiple_current_decisions"
		return result
	}
	if predicateEval.Status == "broken" {
		result.Verdict = "review"
		result.Recommendation = "revalidate"
		result.Reason = predicateEval.Reason
		return result
	}
	if predicateEval.Status == "needs_review" {
		result.Verdict = "review"
		result.Recommendation = "revalidate"
		result.Reason = predicateEval.Reason
		return result
	}
	if matched != nil && isHistoricalMemory(*matched) {
		if current != nil {
			result.Verdict = "use_replacement"
			result.Recommendation = "use_current_decision"
			result.Reason = "matched_historical_decision"
			result.Replacement = current
			return result
		}
		if targetKind == "memory" {
			result.Verdict = "ignore"
			result.Recommendation = "ignore_memory"
			result.Reason = "memory_is_historical"
			return result
		}
		result.Verdict = "review"
		result.Recommendation = "ask_operator"
		result.Reason = "matched_historical_decision"
		return result
	}
	if current != nil {
		if applyStanceVerdict(&result, query, *current) {
			return result
		}
		result.Verdict = "use"
		result.Recommendation = "use_current_decision"
		result.Reason = "single_current_decision"
		return result
	}
	if len(family) > 0 {
		result.Verdict = "review"
		result.Recommendation = "ask_operator"
		result.Reason = "no_current_decision"
	}
	return result
}

func isDecisionLikeMemory(mem store.Memory) bool {
	if strings.TrimSpace(mem.ClaimKey) == "" || strings.EqualFold(mem.SourceKind, "session_evidence") {
		return false
	}
	return isGuidanceRole(mem.Role)
}

func currentDecision(family []store.Memory) *store.Memory {
	for _, mem := range family {
		if isDecisionLikeMemory(mem) && !isHistoricalMemory(mem) {
			current := mem
			return &current
		}
	}
	return nil
}

func currentDecisionCount(family []store.Memory) int {
	count := 0
	for _, mem := range family {
		if isDecisionLikeMemory(mem) && !isHistoricalMemory(mem) {
			count++
		}
	}
	return count
}

func predicateSourceRecheck(mem *store.Memory) string {
	if mem == nil {
		return ""
	}
	if pred, ok := predicateFromMemory(*mem); ok {
		return firstNonEmpty(pred.RecheckPrompt, pred.Prompt)
	}
	return ""
}

func checkFrontmatter(result checkResult) []kv {
	meta := []kv{
		{k: "verdict", v: result.Verdict},
		{k: "predicate_status", v: result.PredicateStatus},
		{k: "recommendation", v: result.Recommendation},
		{k: "reason", v: result.Reason},
		{k: "predicate_reason", v: result.PredicateDetails.Reason},
		{k: "claim_key", v: result.ClaimKey},
		{k: "decision_stance", v: result.DecisionStance},
		{k: "decision_subject", v: result.DecisionSubject},
		{k: "requested_action", v: result.RequestedAction},
		{k: "advisory", v: result.Advisory},
		{k: "recheck", v: result.RecheckPrompt},
	}
	meta = append(meta,
		kv{k: "channel_imported", v: fmt.Sprintf("%d", channelFreshnessImported(result.ChannelFreshness))},
		kv{k: "channel_errors", v: fmt.Sprintf("%d", channelFreshnessErrors(result.ChannelFreshness))},
	)
	meta = append(meta, channelFriendlyFrontmatter(result.ChannelFreshness)...)
	meta = append(meta, channelOutboxFrontmatter("channel_", result.ChannelFreshness.Outbox)...)
	return meta
}

func renderCheckResult(result checkResult) string {
	if result.Verdict == "no_decision" {
		return "No decision family found.\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "## Decision Check\n\n")
	fmt.Fprintf(&b, "claim_key: %s\n", result.ClaimKey)
	fmt.Fprintf(&b, "verdict: %s\n", result.Verdict)
	fmt.Fprintf(&b, "predicate_status: %s\n", result.PredicateStatus)
	fmt.Fprintf(&b, "recommendation: %s\n", result.Recommendation)
	if result.PredicateDetails.Reason != "" {
		fmt.Fprintf(&b, "predicate_reason: %s\n", result.PredicateDetails.Reason)
	}
	if result.DecisionStance != "" {
		fmt.Fprintf(&b, "decision_stance: %s\n", result.DecisionStance)
	}
	if result.DecisionSubject != "" {
		fmt.Fprintf(&b, "decision_subject: %s\n", result.DecisionSubject)
	}
	if result.RequestedAction != "" {
		fmt.Fprintf(&b, "requested_action: %s\n", result.RequestedAction)
	}
	if result.Advisory != "" {
		fmt.Fprintf(&b, "advisory: %s\n", result.Advisory)
	}
	if result.RecheckPrompt != "" {
		fmt.Fprintf(&b, "recheck: %s\n", result.RecheckPrompt)
	}
	if result.MatchedMemory != nil {
		fmt.Fprintf(&b, "matched_id: %s\n", result.MatchedMemory.ID)
	}
	if result.CurrentDecision != nil {
		fmt.Fprintf(&b, "current_id: %s\n", result.CurrentDecision.ID)
	}
	if result.Replacement != nil {
		fmt.Fprintf(&b, "replacement_id: %s\n", result.Replacement.ID)
	}
	if result.CurrentDecision != nil {
		fmt.Fprintf(&b, "\n## Current Decision\n\n%s\n", memoryBlocks([]store.Memory{*result.CurrentDecision}, 1200, false))
	}
	if result.MatchedMemory != nil && (result.CurrentDecision == nil || result.MatchedMemory.ID != result.CurrentDecision.ID) {
		fmt.Fprintf(&b, "\n## Matched Decision\n\n%s\n", memoryBlocks([]store.Memory{*result.MatchedMemory}, 1200, false))
	}
	history := historicalDecisionMemories(result.Family, result.CurrentDecision, result.MatchedMemory)
	if len(history) > 0 {
		fmt.Fprintf(&b, "\n## Decision History\n\n%s\n", memoryBlocks(history, 2000, false))
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

func applyStanceVerdict(result *checkResult, query string, mem store.Memory) bool {
	meta := decisionMetadataFromMemory(mem)
	stance, err := normalizeDecisionStance(meta.Stance)
	if err != nil || stance == "" || strings.TrimSpace(meta.Subject) == "" {
		return false
	}
	action := requestedAction(query, meta.Subject)
	result.DecisionStance = stance
	result.DecisionSubject = strings.TrimSpace(meta.Subject)
	result.RequestedAction = action
	if action == "unknown" {
		return false
	}
	if stanceActionAligns(stance, action) {
		result.Verdict = "use"
		result.Recommendation = "use_current_decision"
		result.Reason = "request_consistent_with_decision"
		return true
	}
	result.Verdict = "review"
	if stance == "requires" || stance == "forbids" {
		result.Recommendation = "block_action"
	} else {
		result.Recommendation = "ask_operator"
	}
	result.Reason = "request_contradicts_decision"
	return true
}

func stanceActionAligns(stance, action string) bool {
	switch stance {
	case "prefers", "requires":
		return action == "affirm_decision" || action == "adopt_subject"
	case "rejects", "forbids":
		return action == "affirm_decision" || action == "remove_subject" || action == "replace_subject"
	default:
		return false
	}
}

func requestedAction(query, subject string) string {
	q := normalizedActionText(query)
	s := normalizedActionText(subject)
	if q == "" {
		return "unknown"
	}
	for _, cue := range []string{"keep the decision", "stick with the decision", "stick with the call", "follow the policy", "follow the decision", "honor the decision"} {
		if strings.Contains(q, cue) {
			return "affirm_decision"
		}
	}
	if s == "" || !strings.Contains(q, s) {
		return "unknown"
	}
	for _, cue := range []string{"keep avoiding", "continue avoiding", "keep out", "do not use", "don't use", "avoid", "drop", "disable", "turn off", "switch off", "stop", "skip", "remove"} {
		if strings.Contains(q, cue+" "+s) || strings.Contains(q, cue+" the "+s) {
			return "remove_subject"
		}
	}
	for _, cue := range []string{"switch from", "replace", "move from"} {
		if strings.Contains(q, cue+" "+s) || strings.Contains(q, cue+" the "+s) {
			return "replace_subject"
		}
	}
	for _, cue := range []string{"use", "add", "introduce", "enable", "switch to", "file", "send", "register", "treat as", "quote", "keep", "continue", "preserve"} {
		if strings.Contains(q, cue+" "+s) || strings.Contains(q, cue+" the "+s) || strings.Contains(q, cue+" with "+s) {
			return "adopt_subject"
		}
	}
	return "unknown"
}

func normalizedActionText(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "-", " ")
	return strings.Join(strings.Fields(value), " ")
}

func historicalDecisionMemories(family []store.Memory, current, matched *store.Memory) []store.Memory {
	var out []store.Memory
	seen := map[string]bool{}
	add := func(mem store.Memory) {
		if seen[mem.ID] {
			return
		}
		seen[mem.ID] = true
		out = append(out, mem)
	}
	for _, mem := range family {
		if current != nil && mem.ID == current.ID {
			continue
		}
		if matched != nil && mem.ID == matched.ID {
			continue
		}
		if isDecisionLikeMemory(mem) {
			add(mem)
		}
	}
	return out
}

func decisionTrail(ctx context.Context, st *store.Store, sc scope.Scope, limit int) ([]decisionTrailItem, error) {
	if limit <= 0 {
		limit = 8
	}
	memories, err := st.List(ctx, store.ListParams{
		ScopeKind: sc.Kind,
		ScopeID:   sc.ID,
		Limit:     max(limit*8, 40),
		Lifecycle: store.LifecycleAny,
	})
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []decisionTrailItem
	for _, mem := range memories {
		if len(out) >= limit {
			break
		}
		if !isDecisionLikeMemory(mem) || seen[mem.ClaimKey] {
			continue
		}
		seen[mem.ClaimKey] = true
		eval := evaluateDecisionPredicate(ctx, st, sc, mem)
		item := decisionTrailItem{
			ClaimKey:        mem.ClaimKey,
			Validity:        mem.Validity,
			PredicateStatus: eval.Status,
			Decision:        &mem,
			RecheckPrompt:   firstNonEmpty(eval.Prompt, predicateSourceRecheck(&mem)),
			Reason:          eval.Reason,
		}
		out = append(out, item)
	}
	return out, nil
}

func renderDecisionTrail(trail []decisionTrailItem) string {
	if len(trail) == 0 {
		return "## Decision Trail\n\nNo decision trail found."
	}
	var b strings.Builder
	b.WriteString("## Decision Trail\n")
	for _, item := range trail {
		fmt.Fprintf(&b, "\n### %s\n", item.ClaimKey)
		fmt.Fprintf(&b, "validity: %s\n", item.Validity)
		fmt.Fprintf(&b, "predicate_status: %s\n", item.PredicateStatus)
		if item.RecheckPrompt != "" {
			fmt.Fprintf(&b, "recheck: %s\n", item.RecheckPrompt)
		}
		if item.Decision != nil {
			if item.Decision.SourcePath != "" {
				fmt.Fprintf(&b, "source: %s", item.Decision.SourcePath)
				if item.Decision.SourceRef != "" {
					fmt.Fprintf(&b, " %s", item.Decision.SourceRef)
				}
				b.WriteByte('\n')
			}
			fmt.Fprintf(&b, "decision: %s\n", truncateText(oneLine(item.Decision.Content), 320))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func combineWakeDecisionTrail(trail, body string) string {
	trail = strings.TrimSpace(trail)
	body = strings.TrimSpace(body)
	if trail == "" {
		return body
	}
	if body == "" {
		return trail + "\n"
	}
	return trail + "\n\n" + body + "\n"
}
