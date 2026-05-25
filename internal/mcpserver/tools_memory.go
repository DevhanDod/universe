package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Universe/universe/internal/memory"
)

// ============================================================
// Tool: recall_memory
// ============================================================

type RecallMemoryInput struct {
	Query        string   `json:"query,omitempty" jsonschema:"description=Text to search for. Optional if graph_node_ids provided."`
	GraphNodeIDs []string `json:"graph_node_ids,omitempty" jsonschema:"description=Graph node IDs to search by."`
	Categories   []string `json:"categories,omitempty" jsonschema:"description=Filter by category: fix pattern decision failure convention"`
	Limit        int      `json:"limit,omitempty" jsonschema:"description=Max results (default 10)"`
}

type RecallMemoryOutput struct {
	Observations []ObservationSummaryOut `json:"observations"`
	TotalCount   int                     `json:"total_count"`
	SearchMethod string                  `json:"search_method"`
	Message      string                  `json:"message,omitempty"`
}

type ObservationSummaryOut struct {
	ID          string  `json:"id"`
	GraphNodeID string  `json:"graph_node_id"`
	Category    string  `json:"category"`
	Summary     string  `json:"summary"`
	Confidence  float64 `json:"confidence"`
	CreatedAt   string  `json:"created_at"`
	Score       float64 `json:"score"`
}

func (h *Handlers) HandleRecallMemory(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input RecallMemoryInput,
) (*mcp.CallToolResult, RecallMemoryOutput, error) {
	if h.retriever == nil {
		return nil, RecallMemoryOutput{
			Message: "Memory engine not available. Set DATABASE_URL and restart.",
		}, nil
	}

	limit := input.Limit
	if limit == 0 {
		limit = 10
	}

	result, err := h.retriever.Search(memory.SearchQuery{
		Text:                  input.Query,
		GraphNodeIDs:          input.GraphNodeIDs,
		Categories:            input.Categories,
		Limit:                 limit,
		IncludeGraphNeighbors: true,
	})
	if err != nil {
		return nil, RecallMemoryOutput{}, err
	}

	obs := make([]ObservationSummaryOut, len(result.Summaries))
	for i, s := range result.Summaries {
		obs[i] = ObservationSummaryOut{
			ID:          s.ID,
			GraphNodeID: s.GraphNodeID,
			Category:    s.Category,
			Summary:     s.Summary,
			Confidence:  s.Confidence,
			CreatedAt:   s.CreatedAt.Format("2006-01-02T15:04:05Z"),
			Score:       s.Score,
		}
	}

	return nil, RecallMemoryOutput{
		Observations: obs,
		TotalCount:   result.TotalCount,
		SearchMethod: result.SearchMethod,
	}, nil
}

// ============================================================
// Tool: get_observation_details
// ============================================================

type GetObservationDetailsInput struct {
	IDs []string `json:"ids" jsonschema:"required,description=List of observation UUIDs to retrieve full details for"`
}

type GetObservationDetailsOutput struct {
	Observations []ObservationDetailOut `json:"observations"`
	Message      string                 `json:"message,omitempty"`
}

type ObservationDetailOut struct {
	ID          string `json:"id"`
	GraphNodeID string `json:"graph_node_id"`
	Category    string `json:"category"`
	Summary     string `json:"summary"`
	Detail      string `json:"detail"`
	DeveloperID string `json:"developer_id"`
	RepoID      string `json:"repo_id"`
	CreatedAt   string `json:"created_at"`
}

func (h *Handlers) HandleGetObservationDetails(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input GetObservationDetailsInput,
) (*mcp.CallToolResult, GetObservationDetailsOutput, error) {
	if h.retriever == nil {
		return nil, GetObservationDetailsOutput{
			Message: "Memory engine not available. Set DATABASE_URL and restart.",
		}, nil
	}

	observations, err := h.retriever.GetFullObservations(input.IDs)
	if err != nil {
		return nil, GetObservationDetailsOutput{}, err
	}

	out := make([]ObservationDetailOut, len(observations))
	for i, o := range observations {
		out[i] = ObservationDetailOut{
			ID:          o.ID,
			GraphNodeID: o.GraphNodeID,
			Category:    o.Category,
			Summary:     o.Summary,
			Detail:      o.Detail,
			DeveloperID: o.DeveloperID,
			RepoID:      o.RepoID,
			CreatedAt:   o.CreatedAt.Format("2006-01-02T15:04:05Z"),
		}
	}

	return nil, GetObservationDetailsOutput{Observations: out}, nil
}

// ============================================================
// Tool: store_observation
// ============================================================

type StoreObservationInput struct {
	GraphNodeID string `json:"graph_node_id" jsonschema:"required,description=The graph node this observation relates to"`
	Category    string `json:"category" jsonschema:"required,description=Category: fix pattern decision failure convention"`
	Content     string `json:"content" jsonschema:"required,description=The observation text to store."`
	RepoID      string `json:"repo_id,omitempty" jsonschema:"description=Repository identifier"`
	Shared      bool   `json:"shared,omitempty" jsonschema:"description=Make visible to the whole team (default false)"`
}

type StoreObservationOutput struct {
	ID      string `json:"id"`
	Summary string `json:"summary"`
	Message string `json:"message"`
}

func (h *Handlers) HandleStoreObservation(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input StoreObservationInput,
) (*mcp.CallToolResult, StoreObservationOutput, error) {
	if h.memoryStore == nil {
		return nil, StoreObservationOutput{
			Message: "Memory engine not available. Set DATABASE_URL and restart.",
		}, nil
	}

	obs := memory.Observation{
		GraphNodeID: input.GraphNodeID,
		Category:    input.Category,
		Summary:     input.Content,
		RepoID:      input.RepoID,
		Shared:      input.Shared,
		Confidence:  1.0,
		DeveloperID: "cursor-agent",
	}
	if obs.RepoID == "" {
		obs.RepoID = "default"
	}

	stored, err := h.memoryStore.InsertObservation(obs)
	if err != nil {
		return nil, StoreObservationOutput{}, err
	}

	return nil, StoreObservationOutput{
		ID:      stored.ID,
		Summary: stored.Summary,
		Message: "Observation stored and will be recalled in future sessions.",
	}, nil
}
