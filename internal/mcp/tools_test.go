package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

// ============================================================
// SERVER TESTS
// ============================================================

func TestServer_Initialize(t *testing.T) {
	reg := &Registry{}
	srv := &Server{tools: reg, in: strings.NewReader(""), out: nil}
	_ = srv // just confirm it constructs without panic
}

func TestServer_HandleInitialize(t *testing.T) {
	reg := &Registry{}
	var out strings.Builder
	srv := &Server{tools: reg, in: strings.NewReader(""), out: &out}

	srv.handle(rpcRequest{
		JSONRPC: "2.0",
		ID:      float64(1),
		Method:  "initialize",
	})

	if !strings.Contains(out.String(), "protocolVersion") {
		t.Errorf("expected protocolVersion in response, got: %s", out.String())
	}
	if !strings.Contains(out.String(), "universe") {
		t.Errorf("expected server name in response, got: %s", out.String())
	}
}

func TestServer_HandleToolsList(t *testing.T) {
	reg := &Registry{}
	reg.Register(ToolDef{
		Name:        "test_tool",
		Description: "a test tool",
		InputSchema: map[string]interface{}{"type": "object"},
	}, func(args json.RawMessage) (interface{}, error) {
		return TextContent("ok"), nil
	})

	var out strings.Builder
	srv := &Server{tools: reg, in: strings.NewReader(""), out: &out}
	srv.handle(rpcRequest{JSONRPC: "2.0", ID: float64(2), Method: "tools/list"})

	if !strings.Contains(out.String(), "test_tool") {
		t.Errorf("expected test_tool in tools list, got: %s", out.String())
	}
}

func TestServer_HandleToolsCall(t *testing.T) {
	reg := &Registry{}
	reg.Register(ToolDef{Name: "echo", Description: "echoes input"}, func(args json.RawMessage) (interface{}, error) {
		return TextContent("echoed"), nil
	})

	var out strings.Builder
	srv := &Server{tools: reg, in: strings.NewReader(""), out: &out}

	params, _ := json.Marshal(map[string]interface{}{
		"name":      "echo",
		"arguments": map[string]string{},
	})
	srv.handle(rpcRequest{JSONRPC: "2.0", ID: float64(3), Method: "tools/call", Params: params})

	if !strings.Contains(out.String(), "echoed") {
		t.Errorf("expected echoed in response, got: %s", out.String())
	}
}

func TestServer_HandleUnknownMethod(t *testing.T) {
	reg := &Registry{}
	var out strings.Builder
	srv := &Server{tools: reg, in: strings.NewReader(""), out: &out}
	srv.handle(rpcRequest{JSONRPC: "2.0", ID: float64(4), Method: "unknown/method"})

	if !strings.Contains(out.String(), "method not found") {
		t.Errorf("expected method not found error, got: %s", out.String())
	}
}

func TestServer_HandlePing(t *testing.T) {
	reg := &Registry{}
	var out strings.Builder
	srv := &Server{tools: reg, in: strings.NewReader(""), out: &out}
	srv.handle(rpcRequest{JSONRPC: "2.0", ID: float64(5), Method: "ping"})

	// ping returns empty result {}
	var resp rpcResponse
	if err := json.Unmarshal([]byte(strings.TrimSpace(out.String())), &resp); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}
	if resp.Error != nil {
		t.Errorf("expected no error for ping, got: %v", resp.Error)
	}
}

func TestServer_UnknownTool(t *testing.T) {
	reg := &Registry{}
	var out strings.Builder
	srv := &Server{tools: reg, in: strings.NewReader(""), out: &out}

	params, _ := json.Marshal(map[string]interface{}{
		"name":      "nonexistent_tool",
		"arguments": map[string]string{},
	})
	srv.handle(rpcRequest{JSONRPC: "2.0", ID: float64(6), Method: "tools/call", Params: params})

	// should return tool error content, not a JSON-RPC error
	if !strings.Contains(out.String(), "unknown tool") {
		t.Errorf("expected 'unknown tool' in response, got: %s", out.String())
	}
}

// ============================================================
// REGISTRY TESTS
// ============================================================

func TestRegistry_RegisterAndCall(t *testing.T) {
	reg := &Registry{}
	reg.Register(ToolDef{Name: "add", Description: "adds"}, func(args json.RawMessage) (interface{}, error) {
		return TextContent("result"), nil
	})

	result, err := reg.Call("add", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	content, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", result)
	}
	_ = content
}

func TestRegistry_CallUnknown(t *testing.T) {
	reg := &Registry{}
	_, err := reg.Call("missing", json.RawMessage(`{}`))
	if err == nil {
		t.Error("expected error for unknown tool")
	}
}

func TestRegistry_Definitions(t *testing.T) {
	reg := &Registry{}
	reg.Register(ToolDef{Name: "t1"}, nil)
	reg.Register(ToolDef{Name: "t2"}, nil)

	defs := reg.Definitions()
	if len(defs) != 2 {
		t.Errorf("expected 2 definitions, got %d", len(defs))
	}
}

// ============================================================
// TOOL HANDLER TESTS (nil dependencies — no DB needed)
// ============================================================

func TestRouteToolNilMatchers(t *testing.T) {
	// router with nil skill/memory/graph matchers
	reg := &Registry{}
	// We can't easily create a router here without importing orchestrator.
	// The tool handlers with nil dependencies are tested in tools_memory and tools_skill.
	_ = reg
}

func TestMemoryToolNilRetriever(t *testing.T) {
	reg := &Registry{}
	RegisterRecallMemory(reg, nil)

	result, err := reg.Call("universe_recall_memory", json.RawMessage(`{"graph_node_ids":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	content := result.(map[string]interface{})
	items := content["content"].([]map[string]string)
	if items[0]["text"] != "[]" {
		t.Errorf("expected empty array, got: %s", items[0]["text"])
	}
}

func TestSkillToolNilStore(t *testing.T) {
	reg := &Registry{}
	RegisterGetSkill(reg, nil, nil)

	result, err := reg.Call("universe_get_skill", json.RawMessage(`{"task_text":"fix bug"}`))
	if err != nil {
		t.Fatal(err)
	}
	content := result.(map[string]interface{})
	items := content["content"].([]map[string]string)
	if !strings.Contains(items[0]["text"], `"found":false`) && !strings.Contains(items[0]["text"], `"found": false`) {
		t.Errorf("expected found:false, got: %s", items[0]["text"])
	}
}

func TestTextContent(t *testing.T) {
	m := TextContent("hello world")
	items, ok := m["content"].([]map[string]string)
	if !ok || len(items) == 0 {
		t.Fatal("expected content array")
	}
	if items[0]["text"] != "hello world" {
		t.Errorf("expected 'hello world', got %q", items[0]["text"])
	}
	if items[0]["type"] != "text" {
		t.Errorf("expected type 'text', got %q", items[0]["type"])
	}
}
