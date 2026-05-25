package mcp

import (
	"encoding/json"
	"fmt"
)

// ToolDef describes one MCP tool — name, description, and JSON Schema for inputs.
type ToolDef struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema interface{} `json:"inputSchema"`
}

// HandlerFunc is called when a tool is invoked.
// args is the raw JSON of the tool's arguments.
// Returns the MCP content response or an error.
type HandlerFunc func(args json.RawMessage) (interface{}, error)

type entry struct {
	def     ToolDef
	handler HandlerFunc
}

// Registry holds all registered tools.
type Registry struct {
	tools []entry
}

// Register adds a tool to the registry.
func (r *Registry) Register(def ToolDef, handler HandlerFunc) {
	r.tools = append(r.tools, entry{def: def, handler: handler})
}

// Definitions returns all tool definitions for tools/list.
func (r *Registry) Definitions() []ToolDef {
	out := make([]ToolDef, len(r.tools))
	for i, e := range r.tools {
		out[i] = e.def
	}
	return out
}

// Call dispatches a tools/call request to the correct handler.
func (r *Registry) Call(name string, args json.RawMessage) (interface{}, error) {
	for _, e := range r.tools {
		if e.def.Name == name {
			return e.handler(args)
		}
	}
	return nil, fmt.Errorf("unknown tool: %s", name)
}

// jsonSchema is a convenience helper for building simple JSON Schema objects.
func jsonSchema(props map[string]interface{}, required []string) map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": props,
		"required":   required,
	}
}

func strProp(desc string) map[string]string {
	return map[string]string{"type": "string", "description": desc}
}

func arrStrProp(desc string) map[string]interface{} {
	return map[string]interface{}{
		"type":        "array",
		"description": desc,
		"items":       map[string]string{"type": "string"},
	}
}
