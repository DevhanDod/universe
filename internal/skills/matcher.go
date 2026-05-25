package skills

import (
	"math/rand"
	"sort"
	"strings"
	"sync"
)

// EmbedFunc converts text to a vector embedding.
type EmbedFunc func(text string) ([]float32, error)

// Matcher finds the best skill for a given task.
type Matcher struct {
	store    *Store
	embedder EmbedFunc
	config   Config
}

// NewMatcher creates a new Matcher.
func NewMatcher(store *Store, embedder EmbedFunc, config Config) *Matcher {
	return &Matcher{store: store, embedder: embedder, config: config}
}

type scored struct {
	summary       SkillSummary
	graphOverlap  float64
	graphScore    float64
	keywordScore  float64
	semanticScore float64
}

// Match finds the best skill for a task.
func (m *Matcher) Match(query MatchQuery) (*MatchResult, error) {
	if query.Limit <= 0 {
		query.Limit = 5
	}

	// STEP 1: Exploration check
	if rand.Float64() < m.config.ExplorationRate {
		return &MatchResult{ExplorationTriggered: true, SearchMethod: "none"}, nil
	}

	fetchLimit := query.Limit * 3
	if fetchLimit < 10 {
		fetchLimit = 10
	}

	// STEP 2: Run three searches in parallel
	type results struct {
		graph    []Skill
		keyword  []SkillSummary
		semantic []SkillSummary
		graphErr    error
		keywordErr  error
		semanticErr error
	}
	var res results
	var wg sync.WaitGroup

	if len(query.GraphNodeIDs) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res.graph, res.graphErr = m.store.GetByGraphNodes(query.GraphNodeIDs, query.Language, m.config.MinConfidenceForMatch, fetchLimit)
		}()
	}

	if query.TaskText != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res.keyword, res.keywordErr = m.store.SearchKeyword(query.TaskText, m.config.MinConfidenceForMatch, fetchLimit)
		}()
		if m.embedder != nil {
			wg.Add(1)
			go func() {
				defer wg.Done()
				embedding, err := m.embedder(query.TaskText)
				if err == nil {
					res.semantic, res.semanticErr = m.store.SearchSemantic(embedding, m.config.MinConfidenceForMatch, fetchLimit)
				}
			}()
		}
	}

	wg.Wait()

	// STEP 3: Merge and deduplicate
	merged := make(map[string]*scored)

	for _, sk := range res.graph {
		overlap := CalculateGraphOverlap(sk.GraphNodeIDs, query.GraphNodeIDs)
		sum := skillToSummary(sk)
		sum.GraphOverlap = overlap
		merged[sk.ID] = &scored{
			summary:      sum,
			graphOverlap: overlap,
			graphScore:   overlap,
		}
	}

	for _, sum := range res.keyword {
		if e, ok := merged[sum.ID]; ok {
			e.keywordScore = sum.SearchScore
		} else {
			merged[sum.ID] = &scored{summary: sum, keywordScore: sum.SearchScore}
		}
	}

	for _, sum := range res.semantic {
		if e, ok := merged[sum.ID]; ok {
			e.semanticScore = sum.SearchScore
		} else {
			merged[sum.ID] = &scored{summary: sum, semanticScore: sum.SearchScore}
		}
	}

	// STEP 4: Weighted score + filter
	type ranked struct {
		s     *scored
		score float64
	}
	var all []ranked

	for _, entry := range merged {
		final := (entry.graphScore * m.config.GraphScoreWeight) +
			(entry.keywordScore * m.config.KeywordScoreWeight) +
			(entry.semanticScore * m.config.SemanticScoreWeight)

		// STEP 5: Filter
		sum := entry.summary

		// frozen → skip
		if sum.IsFrozen {
			continue
		}
		// language mismatch
		if query.Language != "" && sum.Language != "" && sum.Language != query.Language {
			continue
		}
		// confidence
		if sum.Confidence < m.config.MinConfidenceForMatch {
			continue
		}
		// success rate
		if sum.TimesApplied > 0 && sum.SuccessRate < m.config.MinSuccessRateForMatch {
			continue
		}
		// graph overlap (only if graph nodes provided)
		if len(query.GraphNodeIDs) > 0 && entry.graphOverlap < m.config.MinGraphOverlapForMatch {
			continue
		}
		// negative tags
		if CheckNegativeTags(sum.NegativeTags, query.Language, query.Complexity, "") {
			continue
		}
		// stale penalty: reduce score by 50% but keep
		for _, tag := range sum.NegativeTags {
			if tag.Context == "graph_changed" {
				final *= 0.5
				break
			}
		}

		// STEP 6: Complexity weighting
		if query.Complexity == "complex" && sum.TimesApplied > 0 {
			complexApplied := sum.SuccessByComplexity.Complex.Applied
			complexSucceeded := sum.SuccessByComplexity.Complex.Succeeded
			if complexApplied > 0 {
				complexRate := float64(complexSucceeded) / float64(complexApplied)
				if sum.SuccessRate > 0 {
					final *= complexRate / sum.SuccessRate
				}
			}
		}

		sum.SearchScore = final
		all = append(all, ranked{s: entry, score: final})
	}

	// STEP 7: Sort and return
	sort.Slice(all, func(i, j int) bool {
		return all[i].score > all[j].score
	})
	if len(all) > query.Limit {
		all = all[:query.Limit]
	}

	candidates := make([]SkillSummary, len(all))
	for i, r := range all {
		r.s.summary.SearchScore = r.score
		candidates[i] = r.s.summary
	}

	var best *SkillSummary
	if len(candidates) > 0 && candidates[0].SearchScore > 0.3 {
		cp := candidates[0]
		best = &cp
	}

	method := matchMethod(len(res.graph) > 0, len(res.keyword) > 0, len(res.semantic) > 0)
	return &MatchResult{
		BestMatch:    best,
		Candidates:   candidates,
		SearchMethod: method,
	}, nil
}

// CalculateGraphOverlap computes the fraction of query nodes covered by the skill.
func CalculateGraphOverlap(skillNodes []string, queryNodes []string) float64 {
	if len(queryNodes) == 0 {
		return 0
	}
	set := make(map[string]bool, len(skillNodes))
	for _, n := range skillNodes {
		set[n] = true
	}
	matches := 0
	for _, n := range queryNodes {
		if set[n] {
			matches++
		}
	}
	return float64(matches) / float64(len(queryNodes))
}

// CheckNegativeTags returns true if the skill should be skipped based on its negative tags.
func CheckNegativeTags(tags []NegativeTag, language, complexity, repoID string) bool {
	for _, tag := range tags {
		ctx := strings.ToLower(tag.Context)
		if language != "" && strings.Contains(ctx, strings.ToLower(language)+" repo") {
			return true
		}
		if complexity != "" && strings.Contains(ctx, complexity) {
			return true
		}
		if repoID != "" && strings.Contains(ctx, strings.ToLower(repoID)) {
			return true
		}
	}
	return false
}

func skillToSummary(sk Skill) SkillSummary {
	sum := SkillSummary{
		ID:          sk.ID,
		Name:        sk.Name,
		Version:     sk.Version,
		Evolution:   sk.Evolution,
		TriggerDesc: sk.TriggerDesc,
		Language:    sk.Language,
		Confidence:  sk.Confidence,
		IsFrozen:    sk.IsFrozen,
		NegativeTags: sk.NegativeTags,
		SuccessByComplexity: sk.SuccessByComplexity,
	}
	if sk.TimesApplied > 0 {
		sum.SuccessRate = float64(sk.TimesSucceeded) / float64(sk.TimesApplied)
	}
	return sum
}

func matchMethod(hasGraph, hasKeyword, hasSemantic bool) string {
	count := 0
	if hasGraph { count++ }
	if hasKeyword { count++ }
	if hasSemantic { count++ }
	if count >= 2 { return "hybrid" }
	if hasGraph { return "graph_only" }
	if hasKeyword { return "keyword_only" }
	if hasSemantic { return "semantic_only" }
	return "none"
}
