package main

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	exe, err := exec.LookPath("universe.exe")
	if err != nil {
		exe = "./universe.exe"
	}

	transport := &mcp.CommandTransport{
		Command: exec.Command(exe, "mcp", "--repo", "."),
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "mcp-test", Version: "1.0"}, nil)

	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		log.Fatalf("connect failed: %v", err)
	}
	defer session.Close()

	// List tools
	resp, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		log.Fatalf("tools/list failed: %v", err)
	}

	fmt.Printf("✅ MCP handshake successful\n")
	fmt.Printf("   Tools registered: %d\n", len(resp.Tools))
	for _, t := range resp.Tools {
		fmt.Printf("   - %s\n", t.Name)
	}

	// Test list_skills tool call
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "list_skills",
		Arguments: map[string]any{},
	})
	if err != nil {
		log.Fatalf("list_skills call failed: %v", err)
	}
	if result.IsError {
		fmt.Printf("⚠️  list_skills returned tool error (expected if no DB)\n")
	} else {
		fmt.Printf("✅ list_skills responded with %d content blocks\n", len(result.Content))
	}
}
