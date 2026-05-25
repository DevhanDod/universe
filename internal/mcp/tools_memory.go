package mcp

import (
	"encoding/json"

	"github.com/Universe/universe/internal/memory"
)

// recallMemoryInput is the argument schema for universe_recall_memory.
type recallMemoryInput struct {
	GraphNodeIDs []string `json:"graph_node_ids"`
	DeveloperID  string   `json:"developer_id"`
	TaskText     string   `json:"task_text"`
}

// RegisterRecallMemory adds the universe_recall_memory tool to the registry.
// retriever may be nil — returns an empty result when no DB is configured.
func RegisterRecallMemory(reg *Registry, retriever *memory.Retriever) {
	reg.Register(ToolDef{
		Name: "universe_recall_memory",
		Description: "Search past observations stored from previous sessions. " +
			"Returns relevant observations ranked by graph overlap + keyword match. " +
			"Returns an empty array if no memory is available. Safe to call always.",
		InputSchema: jsonSchema(map[string]interface{}{
			"graph_node_ids": arrStrProp("Graph node IDs to look up memory for (optional)"),
			"developer_id":   strProp("Developer identifier (optional)"),
			"task_text":      strProp("Task description for keyword matching (optional)"),
		}, []string{}),
	}, func(args json.RawMessage) (interface{}, error) {
		var in recallMemoryInput
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, err
		}

		if retriever == nil {
			return TextContent("[]"), nil
		}

		result, err := retriever.Search(memory.SearchQuery{
			GraphNodeIDs:          in.GraphNodeIDs,
			Text:                  in.TaskText,
			DeveloperID:           in.DeveloperID,
			IncludeGraphNeighbors: true,
			Limit:                 10,
		})
		if err != nil {
			// non-fatal — return empty rather than surfacing a DB error to the agent
			return TextContent("[]"), nil
		}

		if len(result.Summaries) == 0 {
			return TextContent("[]"), nil
		}

		data, _ := json.MarshalIndent(result.Summaries, "", "  ")
		return TextContent(string(data)), nil
	})
}
