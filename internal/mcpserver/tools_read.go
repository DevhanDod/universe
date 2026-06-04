package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Universe/universe/internal/graph"
	"github.com/Universe/universe/internal/memory"
	"github.com/Universe/universe/internal/models"
	"github.com/Universe/universe/internal/skills"
)

// v0.2.8 MCP read tools. Reads are MCP again because shell commands
// were getting bypassed — when the agent doesn't see a tool in its
// schema list it reaches for Read/Grep instead. The five tools below
// + the unified universe_context are the only structured surface.
// All responses are compact plain text via response.go formatters.

// ──────────────────────────────────────────────────────────────────────
// universe_query
// ──────────────────────────────────────────────────────────────────────

type QueryInputV28 struct {
	Name string `json:"name"`
}

type TextOutput struct {
	Text string `json:"text"`
}

func (h *Handlers) HandleQueryV28(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input QueryInputV28,
) (*mcp.CallToolResult, TextOutput, error) {
	if h.graph == nil {
		return nil, TextOutput{Text: "Graph not loaded. Run 'universe init'."}, nil
	}
	nodes := h.graph.SearchNodes(input.Name, 5)
	var node *models.Node
	if n := h.graph.GetNode(input.Name); n != nil {
		node = n
	} else {
		for _, candidate := range nodes {
			if strings.EqualFold(candidate.Name, input.Name) {
				node = candidate
				break
			}
		}
		if node == nil && len(nodes) > 0 {
			node = nodes[0]
		}
	}
	if node == nil {
		return nil, TextOutput{
			Text: fmt.Sprintf("No node found for %q. Try universe_search.", input.Name),
		}, nil
	}
	return nil, TextOutput{
		Text: FormatNodeResponse(node, h.graph, DefaultResponseConfig()),
	}, nil
}

// ──────────────────────────────────────────────────────────────────────
// universe_impact
// ──────────────────────────────────────────────────────────────────────

type ImpactInputV28 struct {
	Name          string  `json:"name"`
	Direction     string  `json:"direction,omitempty"` // upstream | downstream
	MaxDepth      int     `json:"max_depth,omitempty"`
	MinConfidence float64 `json:"min_confidence,omitempty"`
}

func (h *Handlers) HandleImpactV28(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input ImpactInputV28,
) (*mcp.CallToolResult, TextOutput, error) {
	if h.graph == nil {
		return nil, TextOutput{Text: "Graph not loaded."}, nil
	}
	if input.MaxDepth <= 0 {
		input.MaxDepth = 3
	}
	if input.MinConfidence <= 0 {
		input.MinConfidence = 0.5
	}
	if input.Direction == "" {
		input.Direction = "upstream"
	}
	node := h.graph.GetNode(input.Name)
	if node == nil {
		nodes := h.graph.SearchNodes(input.Name, 1)
		if len(nodes) > 0 {
			node = nodes[0]
		}
	}
	if node == nil {
		return nil, TextOutput{Text: fmt.Sprintf("No node found for %q.", input.Name)}, nil
	}
	var affected []graph.AffectedNode
	if input.Direction == "downstream" {
		affected = h.graph.GetDependenciesFiltered(node.ID, input.MinConfidence, input.MaxDepth)
	} else {
		affected = h.graph.GetDependentsFiltered(node.ID, input.MinConfidence, input.MaxDepth)
	}
	return nil, TextOutput{
		Text: FormatImpactResponse(node, affected, DefaultResponseConfig()),
	}, nil
}

// ──────────────────────────────────────────────────────────────────────
// universe_search
// ──────────────────────────────────────────────────────────────────────

type SearchInputV28 struct {
	Query string `json:"query"`
	Limit int    `json:"limit,omitempty"`
}

func (h *Handlers) HandleSearchV28(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input SearchInputV28,
) (*mcp.CallToolResult, TextOutput, error) {
	if h.graph == nil {
		return nil, TextOutput{Text: "Graph not loaded."}, nil
	}
	if input.Limit <= 0 || input.Limit > 25 {
		input.Limit = 10
	}
	nodes := h.graph.SearchNodes(input.Query, input.Limit)
	return nil, TextOutput{
		Text: FormatNodeList(nodes, input.Query, DefaultResponseConfig()),
	}, nil
}

// ──────────────────────────────────────────────────────────────────────
// universe_recall
// ──────────────────────────────────────────────────────────────────────

type RecallInputV28 struct {
	Query string `json:"query"`
	Node  string `json:"node,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

func (h *Handlers) HandleRecallV28(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input RecallInputV28,
) (*mcp.CallToolResult, TextOutput, error) {
	if h.memoryStore == nil {
		return nil, TextOutput{Text: "Memory not configured."}, nil
	}
	if input.Limit <= 0 {
		input.Limit = 5
	}
	var rows []memory.ObservationSummary
	var err error
	if input.Node != "" {
		rows, err = h.memoryStore.GetByGraphNode(input.Node, "", input.Limit)
	} else {
		rows, err = h.memoryStore.SearchKeyword(input.Query, "", input.Limit)
	}
	if err != nil {
		return nil, TextOutput{Text: "Memory lookup failed: " + err.Error()}, nil
	}
	if len(rows) == 0 {
		return nil, TextOutput{Text: "No observations found."}, nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Observations (%d):\n", len(rows))
	for _, r := range rows {
		fmt.Fprintf(&b, "  [%s] %s (id=%s node=%s)\n",
			r.CreatedAt.Format("Jan 2"), r.Summary, r.ID, r.GraphNodeID)
	}
	return nil, TextOutput{Text: b.String()}, nil
}

// ──────────────────────────────────────────────────────────────────────
// universe_skill_find
// ──────────────────────────────────────────────────────────────────────

type SkillFindInputV28 struct {
	Query string `json:"query"`
}

func (h *Handlers) HandleSkillFindV28(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input SkillFindInputV28,
) (*mcp.CallToolResult, TextOutput, error) {
	if h.skillMatcher == nil {
		return nil, TextOutput{Text: "Skills not configured."}, nil
	}
	res, err := h.skillMatcher.Match(skills.MatchQuery{
		TaskText: input.Query,
		Limit:    3,
	})
	if err != nil {
		return nil, TextOutput{Text: "Skill match failed: " + err.Error()}, nil
	}
	if res == nil || res.BestMatch == nil {
		return nil, TextOutput{Text: "No matching skill found."}, nil
	}
	s := res.BestMatch
	var b strings.Builder
	fmt.Fprintf(&b, "Skill: %s v%d — %.0f%% success (%d uses)\n",
		s.Name, s.Version, s.SuccessRate*100, s.TimesApplied)
	if s.RequiresVerification {
		b.WriteString("REQUIRES VERIFICATION before use.\n")
	}
	return nil, TextOutput{Text: b.String()}, nil
}

// unusedRefs keeps the imports anchored when handlers below only
// reference one symbol from each package.
var _ = memory.ObservationSummary{}
var _ = models.NodeFunction
