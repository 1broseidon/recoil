package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/1broseidon/recoil/internal/scope"
	"github.com/1broseidon/recoil/internal/store"
)

const predicateMetadataKey = "predicate"

type decisionPredicate struct {
	Tier          string `json:"tier,omitempty"`
	Kind          string `json:"kind,omitempty"`
	Status        string `json:"status,omitempty"`
	HoldsWhile    string `json:"holds_while,omitempty"`
	RecheckPrompt string `json:"recheck_prompt,omitempty"`
	ValidUntil    string `json:"valid_until,omitempty"`
	ClaimKey      string `json:"claim_key,omitempty"`
	Validity      string `json:"validity,omitempty"`
	SourcePath    string `json:"source_path,omitempty"`
	SourceHash    string `json:"source_hash,omitempty"`
	Evaluator     string `json:"evaluator,omitempty"`
	Subject       string `json:"subject,omitempty"`
	Operator      string `json:"operator,omitempty"`
	Value         string `json:"value,omitempty"`
	Prompt        string `json:"prompt,omitempty"`
}

type predicateOptions struct {
	holdsWhile string
	recheck    string
	validUntil string
	raw        string
}

type predicateEvaluation struct {
	Predicate decisionPredicate `json:"predicate,omitempty"`
	Status    string            `json:"status"`
	Reason    string            `json:"reason,omitempty"`
	Prompt    string            `json:"prompt,omitempty"`
}

func addPredicateFlags(c flagSet, opts *predicateOptions) {
	c.StringVar(&opts.holdsWhile, "holds-while", "", "semantic predicate describing when this decision applies")
	c.StringVar(&opts.recheck, "recheck", "", "operator-facing question to ask when the predicate needs review")
	c.StringVar(&opts.validUntil, "valid-until", "", "deterministic date predicate; decision applies through this date")
	c.StringVar(&opts.raw, "predicate", "", "predicate fields as comma-separated key=value pairs")
}

type flagSet interface {
	StringVar(*string, string, string, string)
}

func buildDecisionPredicate(opts predicateOptions) (decisionPredicate, bool, error) {
	var pred decisionPredicate
	hasPredicate := false
	if strings.TrimSpace(opts.raw) != "" {
		parsed, err := parsePredicateFields(opts.raw)
		if err != nil {
			return decisionPredicate{}, false, err
		}
		pred = parsed
		hasPredicate = true
	}
	if strings.TrimSpace(opts.validUntil) != "" {
		if pred.Kind != "" && pred.Kind != "valid_until" && pred.Kind != "date_expiry" {
			return decisionPredicate{}, false, fmt.Errorf("conflicting predicate metadata: --valid-until implies kind=valid_until, but --predicate kind=%s was given", pred.Kind)
		}
		pred.Tier = firstNonEmpty(pred.Tier, "deterministic")
		pred.Kind = "valid_until"
		pred.ValidUntil = strings.TrimSpace(opts.validUntil)
		hasPredicate = true
	}
	if strings.TrimSpace(opts.holdsWhile) != "" {
		pred.Tier = firstNonEmpty(pred.Tier, "semantic")
		pred.Kind = firstNonEmpty(pred.Kind, "semantic")
		pred.HoldsWhile = strings.TrimSpace(opts.holdsWhile)
		hasPredicate = true
	}
	if strings.TrimSpace(opts.recheck) != "" {
		pred.RecheckPrompt = strings.TrimSpace(opts.recheck)
		hasPredicate = true
	}
	if !hasPredicate {
		return decisionPredicate{}, false, nil
	}
	pred = normalizeDecisionPredicate(pred)
	return pred, true, nil
}

func parsePredicateFields(raw string) (decisionPredicate, error) {
	var pred decisionPredicate
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			return decisionPredicate{}, fmt.Errorf("--predicate field %q must be key=value", part)
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		switch key {
		case "tier":
			pred.Tier = value
		case "kind":
			pred.Kind = value
		case "status":
			pred.Status = value
		case "holds_while", "holds-while":
			pred.HoldsWhile = value
		case "recheck", "recheck_prompt", "recheck-prompt":
			pred.RecheckPrompt = value
		case "valid_until", "valid-until":
			pred.ValidUntil = value
		case "claim_key", "claim-key":
			pred.ClaimKey = value
		case "validity":
			pred.Validity = value
		case "source_path", "source-path", "source":
			pred.SourcePath = value
		case "source_hash", "source-hash", "hash":
			pred.SourceHash = value
		case "evaluator":
			pred.Evaluator = value
		case "subject":
			pred.Subject = value
		case "operator", "op":
			pred.Operator = value
		case "value":
			pred.Value = value
		case "prompt":
			pred.Prompt = value
		default:
			return decisionPredicate{}, fmt.Errorf("unsupported --predicate field %q", key)
		}
	}
	return pred, nil
}

func normalizeDecisionPredicate(pred decisionPredicate) decisionPredicate {
	pred.Tier = strings.ToLower(strings.TrimSpace(pred.Tier))
	pred.Kind = strings.ToLower(strings.TrimSpace(pred.Kind))
	pred.Status = strings.ToLower(strings.TrimSpace(pred.Status))
	if pred.Kind == "date_expiry" {
		pred.Kind = "valid_until"
	}
	if pred.Kind == "python_version" {
		pred.Kind = "package_version"
		if strings.TrimSpace(pred.Subject) == "" {
			pred.Subject = "requires-python"
		}
	}
	if pred.Kind == "" {
		switch {
		case pred.ValidUntil != "":
			pred.Kind = "valid_until"
		case pred.SourcePath != "" || pred.SourceHash != "":
			pred.Kind = "source_unchanged"
		case pred.ClaimKey != "":
			pred.Kind = "claim_key_status"
		case pred.Evaluator != "":
			pred.Kind = pred.Evaluator
		default:
			pred.Kind = "semantic"
		}
	}
	if pred.Tier == "" {
		switch pred.Kind {
		case "valid_until", "source_unchanged", "claim_key_status", "package_version", "tsconfig_value", "lint_config_value":
			pred.Tier = "deterministic"
		case "external":
			pred.Tier = "external"
		default:
			if pred.Evaluator != "" {
				pred.Tier = "external"
			} else {
				pred.Tier = "semantic"
			}
		}
	}
	if pred.Status == "" {
		pred.Status = "unknown"
	}
	return pred
}

func mergePredicateMetadata(raw string, pred decisionPredicate) (string, error) {
	metadata := map[string]any{}
	if strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &metadata); err != nil {
			return "", fmt.Errorf("--metadata must be a JSON object when predicate flags are used: %w", err)
		}
	}
	metadata[predicateMetadataKey] = pred
	data, err := json.Marshal(metadata)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func predicateFromMemory(mem store.Memory) (decisionPredicate, bool) {
	if strings.TrimSpace(mem.MetadataJSON) == "" {
		return decisionPredicate{}, false
	}
	var metadata map[string]json.RawMessage
	if err := json.Unmarshal([]byte(mem.MetadataJSON), &metadata); err != nil {
		return decisionPredicate{}, false
	}
	raw, ok := metadata[predicateMetadataKey]
	if !ok {
		return decisionPredicate{}, false
	}
	var pred decisionPredicate
	if err := json.Unmarshal(raw, &pred); err != nil {
		return decisionPredicate{}, false
	}
	return normalizeDecisionPredicate(pred), true
}

func evaluateDecisionPredicate(ctx context.Context, st *store.Store, sc scope.Scope, mem store.Memory) predicateEvaluation {
	pred, ok := predicateFromMemory(mem)
	if !ok {
		return predicateEvaluation{Status: "unknown"}
	}
	switch pred.Kind {
	case "valid_until":
		return evaluateValidUntil(pred)
	case "source_unchanged":
		return evaluateSourceUnchanged(pred)
	case "claim_key_status":
		return evaluateClaimKeyStatus(ctx, st, sc, pred)
	case "package_version":
		return evaluatePackageVersion(sc, pred)
	case "tsconfig_value":
		return evaluateTSConfigValue(sc, pred)
	case "lint_config_value":
		return evaluateLintConfigValue(sc, pred)
	default:
		if pred.Tier == "external" || pred.Evaluator != "" {
			return predicateEvaluation{Predicate: pred, Status: "needs_review", Reason: "external_predicate", Prompt: firstNonEmpty(pred.Prompt, pred.RecheckPrompt)}
		}
		return predicateEvaluation{Predicate: pred, Status: firstNonEmpty(pred.Status, "unknown"), Reason: "semantic_predicate", Prompt: pred.RecheckPrompt}
	}
}

func evaluateValidUntil(pred decisionPredicate) predicateEvaluation {
	if strings.TrimSpace(pred.ValidUntil) == "" {
		return predicateEvaluation{Predicate: pred, Status: "unknown", Reason: "missing_valid_until"}
	}
	expires, err := parsePredicateTime(pred.ValidUntil)
	if err != nil {
		return predicateEvaluation{Predicate: pred, Status: "unknown", Reason: "invalid_valid_until"}
	}
	now := time.Now().UTC()
	if now.After(expires) {
		return predicateEvaluation{Predicate: pred, Status: "broken", Reason: "valid_until_expired", Prompt: pred.RecheckPrompt}
	}
	return predicateEvaluation{Predicate: pred, Status: "holds", Reason: "valid_until_holds"}
}

func parsePredicateTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02", value); err == nil {
		return t.Add(24*time.Hour - time.Nanosecond), nil
	}
	return time.Time{}, fmt.Errorf("unsupported time %q", value)
}

func evaluateSourceUnchanged(pred decisionPredicate) predicateEvaluation {
	path := strings.TrimSpace(pred.SourcePath)
	if path == "" {
		return predicateEvaluation{Predicate: pred, Status: "unknown", Reason: "missing_source_path"}
	}
	wantHash := strings.TrimSpace(pred.SourceHash)
	if wantHash == "" {
		return predicateEvaluation{Predicate: pred, Status: "unknown", Reason: "missing_source_hash"}
	}
	if !filepath.IsAbs(path) {
		path = filepath.Clean(path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return predicateEvaluation{Predicate: pred, Status: "broken", Reason: "source_unreadable", Prompt: pred.RecheckPrompt}
	}
	sum := sha256.Sum256(data)
	gotHash := hex.EncodeToString(sum[:])
	if strings.EqualFold(gotHash, wantHash) {
		return predicateEvaluation{Predicate: pred, Status: "holds", Reason: "source_hash_matches"}
	}
	return predicateEvaluation{Predicate: pred, Status: "broken", Reason: "source_hash_changed", Prompt: pred.RecheckPrompt}
}

func evaluateClaimKeyStatus(ctx context.Context, st *store.Store, sc scope.Scope, pred decisionPredicate) predicateEvaluation {
	claimKey := strings.TrimSpace(pred.ClaimKey)
	if claimKey == "" {
		return predicateEvaluation{Predicate: pred, Status: "unknown", Reason: "missing_claim_key"}
	}
	wantValidity := strings.TrimSpace(pred.Validity)
	if wantValidity == "" {
		wantValidity = "active"
	}
	memories, err := st.List(ctx, store.ListParams{
		ScopeKind: sc.Kind,
		ScopeID:   sc.ID,
		ClaimKey:  claimKey,
		Limit:     50,
		Lifecycle: store.LifecycleAny,
	})
	if err != nil {
		return predicateEvaluation{Predicate: pred, Status: "unknown", Reason: "claim_key_lookup_failed"}
	}
	for _, mem := range memories {
		if strings.EqualFold(strings.TrimSpace(mem.Validity), wantValidity) {
			return predicateEvaluation{Predicate: pred, Status: "holds", Reason: "claim_key_status_matches"}
		}
	}
	return predicateEvaluation{Predicate: pred, Status: "broken", Reason: "claim_key_status_missing", Prompt: pred.RecheckPrompt}
}

func evaluatePackageVersion(sc scope.Scope, pred decisionPredicate) predicateEvaluation {
	subject := strings.TrimSpace(pred.Subject)
	if subject == "" {
		return predicateEvaluation{Predicate: pred, Status: "unknown", Reason: "missing_subject"}
	}
	for _, path := range manifestCandidatePaths(sc, pred, []string{"go.mod", "package.json", "pyproject.toml", "setup.cfg"}) {
		actual, ok, err := packageVersionFromManifest(path, subject)
		if err != nil {
			continue
		}
		if !ok {
			continue
		}
		return evaluatePredicateComparison(pred, actual, "package_version_matches", "package_version_mismatch")
	}
	return predicateEvaluation{Predicate: pred, Status: "unknown", Reason: "package_version_missing"}
}

func packageVersionFromManifest(path, subject string) (string, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false, err
	}
	switch strings.ToLower(filepath.Base(path)) {
	case "go.mod":
		return goModVersion(string(data), subject), goModVersion(string(data), subject) != "", nil
	case "package.json":
		return packageJSONVersion(data, subject)
	case "pyproject.toml":
		return pyProjectVersion(string(data), subject), pyProjectVersion(string(data), subject) != "", nil
	case "setup.cfg":
		return setupCfgVersion(string(data), subject), setupCfgVersion(string(data), subject) != "", nil
	default:
		return "", false, nil
	}
}

func goModVersion(raw, subject string) string {
	subject = strings.TrimSpace(subject)
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "go" && subject == "go" {
			return fields[1]
		}
		if len(fields) >= 3 && fields[0] == "require" && fields[1] == subject {
			return strings.TrimPrefix(fields[2], "v")
		}
		if len(fields) >= 2 && fields[0] == subject {
			return strings.TrimPrefix(fields[1], "v")
		}
	}
	return ""
}

func packageJSONVersion(data []byte, subject string) (string, bool, error) {
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return "", false, err
	}
	if subject == "engines.node" {
		if engines, ok := doc["engines"].(map[string]any); ok {
			if value, ok := engines["node"].(string); ok {
				return value, true, nil
			}
		}
	}
	for _, section := range []string{"dependencies", "devDependencies", "peerDependencies", "optionalDependencies"} {
		deps, ok := doc[section].(map[string]any)
		if !ok {
			continue
		}
		if value, ok := deps[subject].(string); ok {
			return value, true, nil
		}
	}
	return "", false, nil
}

func pyProjectVersion(raw, subject string) string {
	subject = strings.TrimSpace(subject)
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if subject == "requires-python" && (key == "requires-python" || key == "python_requires") {
			return value
		}
		if strings.EqualFold(key, subject) {
			return value
		}
	}
	return ""
}

func setupCfgVersion(raw, subject string) string {
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if subject == "requires-python" && (key == "python_requires" || key == "requires-python") {
			return value
		}
	}
	return ""
}

func evaluateTSConfigValue(sc scope.Scope, pred decisionPredicate) predicateEvaluation {
	subject := strings.TrimSpace(pred.Subject)
	if subject == "" {
		return predicateEvaluation{Predicate: pred, Status: "unknown", Reason: "missing_subject"}
	}
	path := firstManifestCandidatePath(sc, pred, "tsconfig.json")
	if path == "" {
		return predicateEvaluation{Predicate: pred, Status: "unknown", Reason: "tsconfig_missing"}
	}
	doc, err := loadTSConfig(path, 0)
	if err != nil {
		return predicateEvaluation{Predicate: pred, Status: "unknown", Reason: "tsconfig_unreadable"}
	}
	actual, ok := dottedValue(doc, subject)
	if !ok {
		return predicateEvaluation{Predicate: pred, Status: "unknown", Reason: "tsconfig_value_missing"}
	}
	return evaluatePredicateComparison(pred, scalarString(actual), "tsconfig_value_matches", "tsconfig_value_mismatch")
}

func loadTSConfig(path string, depth int) (map[string]any, error) {
	if depth > 1 {
		return nil, fmt.Errorf("tsconfig extends depth exceeded")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	extends, _ := doc["extends"].(string)
	if strings.TrimSpace(extends) == "" {
		return doc, nil
	}
	basePath := strings.TrimSpace(extends)
	if !filepath.IsAbs(basePath) {
		basePath = filepath.Join(filepath.Dir(path), basePath)
	}
	if filepath.Ext(basePath) == "" {
		basePath += ".json"
	}
	base, err := loadTSConfig(basePath, depth+1)
	if err != nil {
		return doc, nil
	}
	return mergeJSONObjects(base, doc), nil
}

func mergeJSONObjects(base, override map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(override))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range override {
		if bm, ok := out[k].(map[string]any); ok {
			if om, ok := v.(map[string]any); ok {
				out[k] = mergeJSONObjects(bm, om)
				continue
			}
		}
		out[k] = v
	}
	return out
}

func evaluateLintConfigValue(sc scope.Scope, pred decisionPredicate) predicateEvaluation {
	subject := strings.TrimSpace(pred.Subject)
	if subject == "" {
		return predicateEvaluation{Predicate: pred, Status: "unknown", Reason: "missing_subject"}
	}
	for _, path := range manifestCandidatePaths(sc, pred, []string{".golangci.yml", ".golangci.yaml", "pyproject.toml"}) {
		actual, ok, err := lintConfigValue(path, subject)
		if err != nil || !ok {
			continue
		}
		return evaluatePredicateComparison(pred, actual, "lint_config_value_matches", "lint_config_value_mismatch")
	}
	return predicateEvaluation{Predicate: pred, Status: "unknown", Reason: "lint_config_value_missing"}
}

func lintConfigValue(path, subject string) (string, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false, err
	}
	switch strings.ToLower(filepath.Base(path)) {
	case ".golangci.yml", ".golangci.yaml":
		return simpleYAMLValue(string(data), subject)
	case "pyproject.toml":
		return simpleTOMLValue(string(data), subject)
	default:
		return "", false, nil
	}
}

func simpleYAMLValue(raw, subject string) (string, bool, error) {
	parts := strings.Split(subject, ".")
	if len(parts) == 0 {
		return "", false, nil
	}
	want := parts[len(parts)-1]
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(key) != want {
			continue
		}
		return strings.Trim(strings.TrimSpace(value), `"'`), true, nil
	}
	return "", false, nil
}

func simpleTOMLValue(raw, subject string) (string, bool, error) {
	parts := strings.Split(subject, ".")
	if len(parts) == 0 {
		return "", false, nil
	}
	wantKey := parts[len(parts)-1]
	wantSection := strings.Join(parts[:len(parts)-1], ".")
	section := ""
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.Contains(line, "]") {
			section = strings.TrimSpace(strings.Trim(line, "[]"))
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) != wantKey || section != wantSection {
			continue
		}
		return strings.Trim(strings.TrimSpace(value), `"'`), true, nil
	}
	return "", false, nil
}

func evaluatePredicateComparison(pred decisionPredicate, actual, holdReason, brokenReason string) predicateEvaluation {
	ok, known := comparePredicateValue(actual, pred.Operator, pred.Value)
	if !known {
		return predicateEvaluation{Predicate: pred, Status: "unknown", Reason: "predicate_comparison_unknown", Prompt: pred.RecheckPrompt}
	}
	if ok {
		return predicateEvaluation{Predicate: pred, Status: "holds", Reason: holdReason}
	}
	return predicateEvaluation{Predicate: pred, Status: "broken", Reason: brokenReason, Prompt: pred.RecheckPrompt}
}

func comparePredicateValue(actual, operator, want string) (bool, bool) {
	operator = strings.TrimSpace(operator)
	if operator == "" {
		operator = "=="
	}
	actual = strings.TrimSpace(actual)
	want = strings.TrimSpace(want)
	switch operator {
	case "==", "=":
		return strings.EqualFold(actual, want), true
	case "!=":
		return !strings.EqualFold(actual, want), true
	}
	cmp, ok := compareVersionish(actual, want)
	if !ok {
		return false, false
	}
	switch operator {
	case ">":
		return cmp > 0, true
	case ">=":
		return cmp >= 0, true
	case "<":
		return cmp < 0, true
	case "<=":
		return cmp <= 0, true
	default:
		return false, false
	}
}

var versionNumberRE = regexp.MustCompile(`\d+(?:\.\d+)*`)

func compareVersionish(actual, want string) (int, bool) {
	a := versionNumberRE.FindString(actual)
	b := versionNumberRE.FindString(want)
	if a == "" || b == "" {
		return 0, false
	}
	ap := strings.Split(a, ".")
	bp := strings.Split(b, ".")
	n := len(ap)
	if len(bp) > n {
		n = len(bp)
	}
	for i := 0; i < n; i++ {
		av, bv := 0, 0
		if i < len(ap) {
			av, _ = strconv.Atoi(ap[i])
		}
		if i < len(bp) {
			bv, _ = strconv.Atoi(bp[i])
		}
		if av < bv {
			return -1, true
		}
		if av > bv {
			return 1, true
		}
	}
	return 0, true
}

func manifestCandidatePaths(sc scope.Scope, pred decisionPredicate, names []string) []string {
	if strings.TrimSpace(pred.SourcePath) != "" {
		path := strings.TrimSpace(pred.SourcePath)
		if !filepath.IsAbs(path) && sc.Root != "" {
			path = filepath.Join(sc.Root, path)
		}
		return []string{path}
	}
	root := sc.Root
	if root == "" {
		root = "."
	}
	paths := make([]string, 0, len(names))
	for _, name := range names {
		paths = append(paths, filepath.Join(root, name))
	}
	return paths
}

func firstManifestCandidatePath(sc scope.Scope, pred decisionPredicate, name string) string {
	for _, path := range manifestCandidatePaths(sc, pred, []string{name}) {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

func dottedValue(doc map[string]any, path string) (any, bool) {
	var cur any = doc
	for _, part := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[part]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

func scalarString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case bool:
		return strconv.FormatBool(v)
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return fmt.Sprintf("%v", v)
	}
}
