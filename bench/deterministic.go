package main

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/1broseidon/recoil/internal/retrieval"
	"github.com/1broseidon/recoil/internal/store"
)

type deterministicSession struct {
	sid       string
	date      string
	dateTime  time.Time
	content   string
	user      string
	assistant string
}

type deterministicRanked struct {
	session deterministicSession
	score   float64
	parts   map[string]float64
}

func scoreQuestionDeterministic(q lmeQuestion, topK int, verbose bool) (questionResult, error) {
	result := questionResult{
		QuestionID:    q.QuestionID,
		QuestionType:  q.QuestionType,
		NumSessions:   len(q.HaystackSessionIDs),
		NumAnswerSIDs: len(q.AnswerSessionIDs),
		Abstention:    strings.HasSuffix(q.QuestionID, "_abs"),
	}

	tmpDir, err := os.MkdirTemp("", "recoil-bench-deterministic-")
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

	scopeID := "lme-deterministic:" + q.QuestionID
	ctx := context.Background()

	t0 := time.Now()
	sessions := make([]deterministicSession, 0, len(q.HaystackSessionIDs))
	for i, sid := range q.HaystackSessionIDs {
		if i >= len(q.HaystackSessions) {
			break
		}
		date := ""
		if i < len(q.HaystackDates) {
			date = q.HaystackDates[i]
		}
		ds := buildDeterministicSession(sid, date, q.HaystackSessions[i])
		if strings.TrimSpace(ds.content) == "" {
			continue
		}
		if _, _, err := st.AddMemory(ctx, store.AddMemoryParams{
			Role:       "source",
			Content:    deterministicFTSContent(ds),
			SourceKind: "session",
			SourcePath: sessionPathPrefix + sid,
			SourceRef:  date,
			ScopeKind:  "project",
			ScopeID:    scopeID,
			Validity:   "unknown",
		}); err != nil {
			return result, fmt.Errorf("adding session %s: %w", sid, err)
		}
		sessions = append(sessions, ds)
	}
	result.IngestMillis = time.Since(t0).Milliseconds()

	t1 := time.Now()
	rows, err := st.Search(ctx, store.SearchParams{
		Query:     retrieval.ExpandedQueryText(q.Question),
		ScopeKind: "project",
		ScopeID:   scopeID,
		Limit:     50,
		Lifecycle: store.LifecycleAny,
		QueryDate: q.QuestionDate,
	})
	if err != nil {
		return result, fmt.Errorf("search: %w", err)
	}
	ftsRank := map[string]int{}
	for i, mem := range rows {
		sid := strings.TrimPrefix(mem.SourcePath, sessionPathPrefix)
		if _, ok := ftsRank[sid]; !ok {
			ftsRank[sid] = i + 1
		}
	}

	ranked := rankDeterministicSessions(q, sessions, ftsRank)
	if len(ranked) > topK {
		ranked = ranked[:topK]
	}
	result.SearchMillis = time.Since(t1).Milliseconds()
	result.RetrievedCount = len(ranked)

	if result.Abstention {
		return result, nil
	}
	answerSet := map[string]bool{}
	for _, sid := range q.AnswerSessionIDs {
		answerSet[sid] = true
	}
	hitRank := 0
	for i, item := range ranked {
		result.RetrievedSIDs = append(result.RetrievedSIDs, item.session.sid)
		if answerSet[item.session.sid] && hitRank == 0 {
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
		fmt.Fprintf(os.Stderr, "  %s (%s): r@5=%.0f r@10=%.0f hit_rank=%d sessions=%d\n",
			q.QuestionID, q.QuestionType, result.Recall5, result.Recall10, hitRank, result.NumSessions)
	}
	return result, nil
}

func buildDeterministicSession(sid, date string, turns []lmeTurn) deterministicSession {
	var all, user, assistant strings.Builder
	for _, t := range turns {
		role := strings.ToLower(strings.TrimSpace(t.Role))
		content := strings.TrimSpace(t.Content)
		if role == "" || content == "" {
			continue
		}
		if all.Len() > 0 {
			all.WriteByte('\n')
		}
		all.WriteString(role)
		all.WriteString(": ")
		all.WriteString(content)
		switch role {
		case "user":
			if user.Len() > 0 {
				user.WriteByte('\n')
			}
			user.WriteString(content)
		case "assistant":
			if assistant.Len() > 0 {
				assistant.WriteByte('\n')
			}
			assistant.WriteString(content)
		}
	}
	return deterministicSession{
		sid:       sid,
		date:      date,
		dateTime:  parseLMETime(date),
		content:   all.String(),
		user:      user.String(),
		assistant: assistant.String(),
	}
}

func deterministicFTSContent(s deterministicSession) string {
	var b strings.Builder
	if s.date != "" {
		b.WriteString("session date: ")
		b.WriteString(s.date)
		b.WriteByte('\n')
	}
	b.WriteString(s.content)
	if profile := deterministicProfileText(s); profile != "" {
		b.WriteString("\n\n")
		b.WriteString(profile)
	}
	return b.String()
}

func deterministicProfileText(s deterministicSession) string {
	return retrieval.ProfileText(s.user)
}

func rankDeterministicSessions(q lmeQuestion, sessions []deterministicSession, ftsRank map[string]int) []deterministicRanked {
	expandedQuery := retrieval.ExpandedQueryText(q.Question)
	qtokens := significantTokens(expandedQuery)
	queryLower := strings.ToLower(q.Question)
	queryDate := parseLMETime(q.QuestionDate)
	quoted := quotedPhrases(q.Question)
	names := properNames(q.Question)
	intent := queryIntent(queryLower, q.QuestionType)

	ranked := make([]deterministicRanked, 0, len(sessions))
	for _, s := range sessions {
		contentLower := strings.ToLower(s.content)
		userLower := strings.ToLower(s.user)
		assistantLower := strings.ToLower(s.assistant)
		parts := map[string]float64{}
		if rank, ok := ftsRank[s.sid]; ok {
			parts["fts"] = 1.0 / float64(rank)
		} else {
			// Keep FTS as the spine. Deterministic features may rescue an
			// out-of-pool session, but they should not replace sparse retrieval.
			parts["not_fts_penalty"] = -0.20
		}
		parts["lexical"] = 0.006 * lexicalCoverage(qtokens, contentLower)
		if intent.userFocused {
			parts["user"] = 0.004 * lexicalCoverage(qtokens, userLower)
		}
		if intent.assistantFocused {
			parts["assistant"] = 0.004 * lexicalCoverage(qtokens, assistantLower)
		}
		parts["phrase"] = phraseScore(quoted, contentLower)
		parts["names"] = 0.006 * lexicalCoverage(names, contentLower)
		if intent.preference {
			parts["preference"] = preferenceScore(userLower)
		}
		if intent.update {
			parts["update"] = updateScore(contentLower)
		}
		if intent.temporal {
			parts["temporal"] = temporalScore(queryDate, s.dateTime, queryLower, contentLower, q.QuestionType, parts["lexical"]+parts["user"]+parts["assistant"]+parts["phrase"]+parts["names"])
		}
		score := 0.0
		for _, v := range parts {
			score += v
		}
		ranked = append(ranked, deterministicRanked{session: s, score: score, parts: parts})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if math.Abs(ranked[i].score-ranked[j].score) < 1e-12 {
			return ranked[i].session.date > ranked[j].session.date
		}
		return ranked[i].score > ranked[j].score
	})
	return ranked
}

type deterministicIntent struct {
	userFocused      bool
	assistantFocused bool
	preference       bool
	update           bool
	temporal         bool
}

func queryIntent(q, typ string) deterministicIntent {
	hasAny := func(words ...string) bool {
		for _, w := range words {
			if strings.Contains(q, w) {
				return true
			}
		}
		return false
	}
	return deterministicIntent{
		userFocused:      hasAny(" i ", " my ", " me ", " mine ", "user") || strings.Contains(typ, "user") || strings.Contains(typ, "preference"),
		assistantFocused: hasAny("assistant", "you said", "you tell", "your response") || strings.Contains(typ, "assistant"),
		preference:       strings.Contains(typ, "preference") || hasAny("prefer", "favorite", "favourite", "like better", "rather", "enjoy", "love", "reliable"),
		update:           strings.Contains(typ, "knowledge-update") || hasAny("now", "current", "latest", "update", "change", "changed", "instead", "no longer"),
		temporal:         strings.Contains(typ, "temporal") || hasAny("when", "before", "after", "latest", "last", "first", "recent", "date", "time"),
	}
}

var tokenREBench = regexp.MustCompile(`[a-z0-9]+`)

func significantTokens(s string) []string {
	return retrieval.SignificantTokens(s)
}

func lexicalCoverage(tokens []string, lowerText string) float64 {
	if len(tokens) == 0 || lowerText == "" {
		return 0
	}
	hits := 0
	for _, tok := range tokens {
		if strings.Contains(lowerText, tok) {
			hits++
		}
	}
	return float64(hits) / math.Sqrt(float64(len(tokens))+1)
}

var quotedRE = regexp.MustCompile(`"([^"]{3,120})"|'([^']{3,120})'`)

func quotedPhrases(s string) []string {
	matches := quotedRE.FindAllStringSubmatch(s, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		v := m[1]
		if v == "" {
			v = m[2]
		}
		out = append(out, strings.ToLower(strings.TrimSpace(v)))
	}
	return out
}

func phraseScore(phrases []string, lowerText string) float64 {
	score := 0.0
	for _, p := range phrases {
		if p != "" && strings.Contains(lowerText, p) {
			score += 0.030
		}
	}
	return score
}

var properNameRE = regexp.MustCompile(`\b[A-Z][a-z]{2,}\b`)

func properNames(s string) []string {
	matches := properNameRE.FindAllString(s, -1)
	out := make([]string, 0, len(matches))
	seen := map[string]bool{}
	for _, m := range matches {
		l := strings.ToLower(m)
		if len(retrieval.SignificantTokens(l)) == 0 || seen[l] {
			continue
		}
		seen[l] = true
		out = append(out, l)
	}
	return out
}

func preferenceScore(userLower string) float64 {
	score := 0.0
	for _, p := range []string{"prefer", "favorite", "favourite", "like", "love", "enjoy", "rather", "reliable", "better", "best", "least"} {
		if strings.Contains(userLower, p) {
			score += 0.003
		}
	}
	if strings.Contains(userLower, "i find") || strings.Contains(userLower, "in my experience") {
		score += 0.006
	}
	if score > 0.018 {
		return 0.018
	}
	return score
}

func updateScore(lowerText string) float64 {
	score := 0.0
	for _, p := range []string{"actually", "update", "updated", "changed", "now", "currently", "instead", "no longer", "correction", "latest"} {
		if strings.Contains(lowerText, p) {
			score += 0.003
		}
	}
	if score > 0.015 {
		return 0.015
	}
	return score
}

func temporalScore(queryDate, sessionDate time.Time, queryLower, contentLower, questionType string, lexicalEvidence float64) float64 {
	return retrieval.TemporalScore(queryDate, sessionDate, queryLower, contentLower, questionType, lexicalEvidence)
}

func parseLMETime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	if idx := strings.Index(s, " ("); idx > 0 {
		s = s[:idx] + s[strings.LastIndex(s, ")")+1:]
		s = strings.TrimSpace(s)
	}
	for _, layout := range []string{"2006/01/02 15:04", "2006/01/02", time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
