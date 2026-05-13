package main

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/1broseidon/recoil/internal/store"
)

// hybridConfig carries the knobs for hybrid retrieval. The defaults
// (RRF k=60, pools of 50) match common BEIR/MTEB practice.
type hybridConfig struct {
	client        *OpenRouterClient // used when embedProvider == "openrouter"
	embedProvider string            // "openrouter" | "ollama"
	embedModel    string
	ollamaHost    string // only used when embedProvider == "ollama"
	maxEmbedChars int    // per-input cap before embedding (0 = no truncation)
	chunkChars    int    // when > 0, split each session into chunks of this size and max-pool cosine over chunks
	chunkOverlap  int    // overlap between adjacent chunks (chars)
	fusionK       int    // RRF constant; 60 is the standard
	ftsPoolSize   int    // FTS top-K considered for fusion
	embedPoolSize int    // embedding top-K considered for fusion
}

// truncForEmbedding caps a single embedding input to maxChars. Truncates from
// the end; loses tail context for very long sessions.
func truncForEmbedding(s string, maxChars int) string {
	if maxChars <= 0 || len(s) <= maxChars {
		return s
	}
	return s[:maxChars]
}

// chunkText splits a string into windows of chunkSize chars with overlap chars
// of overlap between adjacent windows. Standard sliding-window chunking —
// preserves all content (no data loss), unlike truncation. If the input is
// shorter than chunkSize, returns it as a single chunk.
func chunkText(s string, chunkSize, overlap int) []string {
	if chunkSize <= 0 || len(s) <= chunkSize {
		return []string{s}
	}
	if overlap < 0 || overlap >= chunkSize {
		overlap = chunkSize / 10
	}
	step := chunkSize - overlap
	chunks := make([]string, 0, (len(s)/step)+1)
	for i := 0; i < len(s); i += step {
		end := i + chunkSize
		if end > len(s) {
			end = len(s)
		}
		chunks = append(chunks, s[i:end])
		if end == len(s) {
			break
		}
	}
	return chunks
}

// embedBatch dispatches the batched embedding call to the configured
// provider. Both branches return (vectors, costUSD, error); Ollama always
// reports 0 cost since it's local.
func (hc *hybridConfig) embedBatch(ctx context.Context, inputs []string) ([][]float64, float64, error) {
	switch hc.embedProvider {
	case "ollama":
		return ollamaEmbedBatch(ctx, hc.ollamaHost, hc.embedModel, inputs)
	case "openrouter", "":
		return hc.client.Embed(ctx, hc.embedModel, inputs)
	default:
		return nil, 0, fmt.Errorf("unknown embed-provider %q", hc.embedProvider)
	}
}

// scoreQuestionHybrid retrieves with FTS5 + embeddings and fuses via RRF.
// The fused ranking is returned in topK, and standard recall@5/recall@10 is
// computed the same way the FTS-only path computes it — so the comparison
// is clean.
func scoreQuestionHybrid(q lmeQuestion, topK int, hc *hybridConfig, verbose bool) (questionResult, error) {
	result := questionResult{
		QuestionID:    q.QuestionID,
		QuestionType:  q.QuestionType,
		NumSessions:   len(q.HaystackSessionIDs),
		NumAnswerSIDs: len(q.AnswerSessionIDs),
		Abstention:    strings.HasSuffix(q.QuestionID, "_abs"),
	}

	tmpDir, err := os.MkdirTemp("", "recoil-bench-hybrid-")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(tmpDir)
	dbPath := filepath.Join(tmpDir, "bench.db")
	st, err := store.Open(dbPath)
	if err != nil {
		return result, err
	}
	defer st.Close()

	scopeID := "lme-hybrid:" + q.QuestionID
	ctx := context.Background()

	// 1. Ingest sessions into recoil store (for FTS5 retrieval).
	// 2. Build parallel slice of (sid, content) for embedding.
	t0 := time.Now()
	sids := make([]string, 0, len(q.HaystackSessionIDs))
	contents := make([]string, 0, len(q.HaystackSessionIDs))
	for i, sid := range q.HaystackSessionIDs {
		if i >= len(q.HaystackSessions) {
			break
		}
		content := formatSession(q.HaystackSessions[i])
		if strings.TrimSpace(content) == "" {
			continue
		}
		date := ""
		if i < len(q.HaystackDates) {
			date = q.HaystackDates[i]
		}
		if _, _, err := st.AddMemory(ctx, store.AddMemoryParams{
			Role:       "source",
			Content:    content,
			SourceKind: "session",
			SourcePath: sessionPathPrefix + sid,
			SourceRef:  date,
			ScopeKind:  "project",
			ScopeID:    scopeID,
			Validity:   "unknown",
		}); err != nil {
			return result, fmt.Errorf("adding session %s: %w", sid, err)
		}
		sids = append(sids, sid)
		contents = append(contents, content)
	}
	result.IngestMillis = time.Since(t0).Milliseconds()

	t1 := time.Now()

	// FTS5 ranks
	rows, err := st.Search(ctx, store.SearchParams{
		Query:     q.Question,
		ScopeKind: "project",
		ScopeID:   scopeID,
		Limit:     hc.ftsPoolSize,
		Lifecycle: store.LifecycleAny,
		QueryDate: q.QuestionDate,
	})
	if err != nil {
		return result, fmt.Errorf("fts search: %w", err)
	}
	ftsRank := map[string]int{} // sid -> 1-based rank
	for i, mem := range rows {
		sid := strings.TrimPrefix(mem.SourcePath, sessionPathPrefix)
		if _, exists := ftsRank[sid]; !exists {
			ftsRank[sid] = i + 1
		}
	}

	// Embedding ranks
	embedCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	// Embed query + all session content. If chunkChars is set, each session is
	// split into windows and the per-session similarity becomes the MAX cosine
	// across its chunks — this lets short-context models (nomic 2K,
	// embeddinggemma 2K) compete with long-context ones (bge-m3 8K) by ensuring
	// no content is silently dropped. With chunkChars=0 we fall back to
	// truncating with maxEmbedChars (simpler but lossy on tail-heavy sessions).
	allInputs := []string{truncForEmbedding(q.Question, hc.maxEmbedChars)}
	chunkOwners := []int{-1} // session index for each input; -1 marks the query slot
	for i, c := range contents {
		var chunks []string
		if hc.chunkChars > 0 {
			chunks = chunkText(c, hc.chunkChars, hc.chunkOverlap)
		} else {
			chunks = []string{truncForEmbedding(c, hc.maxEmbedChars)}
		}
		for _, chunk := range chunks {
			allInputs = append(allInputs, chunk)
			chunkOwners = append(chunkOwners, i)
		}
	}
	vectors, _, err := hc.embedBatch(embedCtx, allInputs)
	if err != nil {
		// Fall back to FTS-only on embedding failure.
		result.SearchMillis = time.Since(t1).Milliseconds()
		applyFTSOnlyRanking(&result, rows, q, topK)
		if verbose {
			fmt.Fprintf(os.Stderr, "  %s embed failed (%v), fell back to FTS-only\n", q.QuestionID, err)
		}
		return result, nil
	}
	queryVec := vectors[0]

	type scored struct {
		sid   string
		score float64
	}
	// Per-session max cosine across that session's chunks.
	bestPerSession := make(map[int]float64, len(contents))
	for j := 1; j < len(vectors); j++ {
		owner := chunkOwners[j]
		sim := cosine(queryVec, vectors[j])
		if cur, ok := bestPerSession[owner]; !ok || sim > cur {
			bestPerSession[owner] = sim
		}
	}
	embedScored := make([]scored, 0, len(bestPerSession))
	for i, score := range bestPerSession {
		embedScored = append(embedScored, scored{sid: sids[i], score: score})
	}
	sort.Slice(embedScored, func(i, j int) bool { return embedScored[i].score > embedScored[j].score })
	if len(embedScored) > hc.embedPoolSize {
		embedScored = embedScored[:hc.embedPoolSize]
	}
	embedRank := map[string]int{}
	for i, s := range embedScored {
		embedRank[s.sid] = i + 1
	}

	// RRF fusion: score(sid) = sum over methods of 1 / (k + rank)
	type fused struct {
		sid   string
		score float64
	}
	allSids := map[string]bool{}
	for sid := range ftsRank {
		allSids[sid] = true
	}
	for sid := range embedRank {
		allSids[sid] = true
	}
	fusedList := make([]fused, 0, len(allSids))
	k := float64(hc.fusionK)
	for sid := range allSids {
		s := 0.0
		if r, ok := ftsRank[sid]; ok {
			s += 1.0 / (k + float64(r))
		}
		if r, ok := embedRank[sid]; ok {
			s += 1.0 / (k + float64(r))
		}
		fusedList = append(fusedList, fused{sid: sid, score: s})
	}
	sort.Slice(fusedList, func(i, j int) bool { return fusedList[i].score > fusedList[j].score })
	if len(fusedList) > topK {
		fusedList = fusedList[:topK]
	}
	result.SearchMillis = time.Since(t1).Milliseconds()
	result.RetrievedCount = len(fusedList)

	if result.Abstention {
		return result, nil
	}
	answerSet := map[string]bool{}
	for _, sid := range q.AnswerSessionIDs {
		answerSet[sid] = true
	}
	hitRank := 0
	for i, f := range fusedList {
		result.RetrievedSIDs = append(result.RetrievedSIDs, f.sid)
		if answerSet[f.sid] && hitRank == 0 {
			hitRank = i + 1
		}
	}
	result.HitRank = hitRank
	if hitRank > 0 && hitRank <= 5 {
		result.Recall5 = 1.0
	}
	if hitRank > 0 && hitRank <= 10 {
		result.Recall10 = 1.0
	}
	if verbose {
		fmt.Fprintf(os.Stderr, "  %s (%s): r@5=%.0f r@10=%.0f hit_rank=%d fts_hits=%d embed_hits=%d\n",
			q.QuestionID, q.QuestionType, result.Recall5, result.Recall10, hitRank,
			len(ftsRank), len(embedRank))
	}
	return result, nil
}

func cosine(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// applyFTSOnlyRanking is the fallback when embedding fails — score recall the
// same way the FTS-only path does so the result row is still comparable.
func applyFTSOnlyRanking(result *questionResult, rows []store.Memory, q lmeQuestion, topK int) {
	if result.Abstention {
		return
	}
	answerSet := map[string]bool{}
	for _, sid := range q.AnswerSessionIDs {
		answerSet[sid] = true
	}
	if len(rows) > topK {
		rows = rows[:topK]
	}
	result.RetrievedCount = len(rows)
	hitRank := 0
	for i, mem := range rows {
		sid := strings.TrimPrefix(mem.SourcePath, sessionPathPrefix)
		result.RetrievedSIDs = append(result.RetrievedSIDs, sid)
		if answerSet[sid] && hitRank == 0 {
			hitRank = i + 1
		}
	}
	result.HitRank = hitRank
	if hitRank > 0 && hitRank <= 5 {
		result.Recall5 = 1.0
	}
	if hitRank > 0 && hitRank <= 10 {
		result.Recall10 = 1.0
	}
}
