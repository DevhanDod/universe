package memory

import (
	"fmt"
	"sort"
	"sync"
)

// EmbedFunc converts text into a vector embedding.
type EmbedFunc func(text string) ([]float32, error)

// GraphQuerier is an interface to the knowledge graph for neighbor lookups.
type GraphQuerier interface {
	GetCallerIDs(nodeID string) ([]string, error)
	GetCalleeIDs(nodeID string) ([]string, error)
}

// Retriever performs 3-way hybrid search: keyword + semantic + graph.
type Retriever struct {
	store        *Store
	embedder     EmbedFunc
	graphQuerier GraphQuerier
	config       Config
}

// NewRetriever creates a new Retriever.
func NewRetriever(store *Store, embedder EmbedFunc, graphQuerier GraphQuerier, config Config) *Retriever {
	return &Retriever{
		store:        store,
		embedder:     embedder,
		graphQuerier: graphQuerier,
		config:       config,
	}
}

type scoredObs struct {
	summary      ObservationSummary
	graphScore   float64
	keywordScore float64
	semanticScore float64
}

// Search performs the full 3-way hybrid search.
// query.DeveloperID is required — memory is personal and scoped per developer.
func (r *Retriever) Search(query SearchQuery) (*SearchResult, error) {
	if query.DeveloperID == "" {
		return nil, fmt.Errorf("DeveloperID is required — memory is personal")
	}
	if query.Limit <= 0 {
		query.Limit = r.config.DefaultSearchLimit
	}
	if query.Limit > r.config.MaxSearchLimit {
		query.Limit = r.config.MaxSearchLimit
	}

	// STEP 1: Expand graph nodes to include neighbors
	expandedNodes := make([]string, 0, len(query.GraphNodeIDs))
	directNodes := make(map[string]bool)
	for _, id := range query.GraphNodeIDs {
		directNodes[id] = true
		expandedNodes = append(expandedNodes, id)
	}

	if len(query.GraphNodeIDs) > 0 && query.IncludeGraphNeighbors && r.graphQuerier != nil {
		seen := make(map[string]bool)
		for _, id := range query.GraphNodeIDs {
			seen[id] = true
		}
		for _, id := range query.GraphNodeIDs {
			callers, _ := r.graphQuerier.GetCallerIDs(id)
			callees, _ := r.graphQuerier.GetCalleeIDs(id)
			for _, nb := range append(callers, callees...) {
				if !seen[nb] {
					seen[nb] = true
					expandedNodes = append(expandedNodes, nb)
				}
			}
		}
	}

	// STEP 2: Run three searches in parallel
	fetchLimit := query.Limit * 2
	if fetchLimit < 20 {
		fetchLimit = 20
	}

	type searchResults struct {
		graph    []ObservationSummary
		keyword  []ObservationSummary
		semantic []ObservationSummary
		graphErr    error
		keywordErr  error
		semanticErr error
	}
	var res searchResults
	var wg sync.WaitGroup

	if len(expandedNodes) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res.graph, res.graphErr = r.store.GetByGraphNodes(expandedNodes, query.DeveloperID, fetchLimit)
		}()
	}

	if query.Text != "" {
		wg.Add(2)
		go func() {
			defer wg.Done()
			res.keyword, res.keywordErr = r.store.SearchKeyword(query.Text, query.DeveloperID, fetchLimit)
		}()
		go func() {
			defer wg.Done()
			if r.embedder != nil {
				embedding, err := r.embedder(query.Text)
				if err == nil {
					res.semantic, res.semanticErr = r.store.SearchSemantic(embedding, query.DeveloperID, fetchLimit)
				}
			}
		}()
	}

	wg.Wait()

	// STEP 3: Merge and deduplicate
	merged := make(map[string]*scoredObs)

	for _, obs := range res.graph {
		score := 1.0
		if !directNodes[obs.GraphNodeID] {
			score = 0.7
		}
		if entry, ok := merged[obs.ID]; ok {
			entry.graphScore = score
		} else {
			cp := obs
			merged[obs.ID] = &scoredObs{summary: cp, graphScore: score}
		}
	}

	for _, obs := range res.keyword {
		if entry, ok := merged[obs.ID]; ok {
			entry.keywordScore = obs.Score
		} else {
			cp := obs
			merged[obs.ID] = &scoredObs{summary: cp, keywordScore: obs.Score}
		}
	}

	for _, obs := range res.semantic {
		if entry, ok := merged[obs.ID]; ok {
			entry.semanticScore = obs.Score
		} else {
			cp := obs
			merged[obs.ID] = &scoredObs{summary: cp, semanticScore: obs.Score}
		}
	}

	// STEP 4: Calculate final weighted score
	type ranked struct {
		obs   ObservationSummary
		score float64
	}
	var all []ranked
	for _, entry := range merged {
		final := (entry.graphScore * r.config.GraphScoreWeight) +
			(entry.keywordScore * r.config.KeywordScoreWeight) +
			(entry.semanticScore * r.config.SemanticScoreWeight)
		s := entry.summary
		s.Score = final
		all = append(all, ranked{obs: s, score: final})
	}

	// STEP 5: Sort and take top N
	sort.Slice(all, func(i, j int) bool {
		return all[i].score > all[j].score
	})
	if len(all) > query.Limit {
		all = all[:query.Limit]
	}

	summaries := make([]ObservationSummary, len(all))
	for i, r := range all {
		summaries[i] = r.obs
	}

	// STEP 6: Update recalled_at async
	for _, s := range summaries {
		id := s.ID
		go func() { _ = r.store.TouchRecalled(id) }()
	}

	// Determine search method used
	method := searchMethod(len(expandedNodes) > 0, query.Text != "" && res.keywordErr == nil, query.Text != "" && len(res.semantic) > 0)

	return &SearchResult{
		Summaries:     summaries,
		TotalCount:    len(merged),
		SearchedNodes: expandedNodes,
		SearchMethod:  method,
	}, nil
}

// GetFullObservations retrieves full observation details for specific IDs.
func (r *Retriever) GetFullObservations(ids []string) ([]Observation, error) {
	return r.store.GetByIDs(ids)
}

// GetSessionContext retrieves all relevant observations for a set of graph nodes.
func (r *Retriever) GetSessionContext(graphNodeIDs []string, developerID string) (*SearchResult, error) {
	return r.Search(SearchQuery{
		GraphNodeIDs:          graphNodeIDs,
		IncludeGraphNeighbors: true,
		DeveloperID:           developerID,
		Limit:                 10,
	})
}

func searchMethod(hasGraph, hasKeyword, hasSemantic bool) string {
	count := 0
	if hasGraph {
		count++
	}
	if hasKeyword {
		count++
	}
	if hasSemantic {
		count++
	}
	if count >= 2 {
		return "hybrid"
	}
	if hasGraph {
		return "graph_only"
	}
	if hasKeyword {
		return "keyword_only"
	}
	if hasSemantic {
		return "semantic_only"
	}
	return "none"
}
