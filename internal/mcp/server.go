// Package mcp implements a minimal MCP (Model Context Protocol) server
// over stdio using JSON-RPC 2.0. Only the tools capability is supported.
package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
)

const protocolVersion = "2024-11-05"

// rpcRequest is an incoming JSON-RPC 2.0 message.
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// rpcResponse is an outgoing JSON-RPC 2.0 message.
type rpcResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	Error   *rpcError   `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Server is the stdio MCP server.
type Server struct {
	tools   *Registry
	in      io.Reader
	out     io.Writer
}

// NewServer creates a server that reads from stdin and writes to stdout.
// All log output must go to stderr — stdout is the MCP wire.
func NewServer(tools *Registry) *Server {
	return &Server{tools: tools, in: os.Stdin, out: os.Stdout}
}

// Run starts the JSON-RPC read loop. Blocks until stdin is closed.
func (s *Server) Run() error {
	scanner := bufio.NewScanner(s.in)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			s.writeError(nil, -32700, "parse error: "+err.Error())
			continue
		}

		// Notifications (no id) — just ack, don't respond
		if req.ID == nil {
			continue
		}

		s.handle(req)
	}

	if err := scanner.Err(); err != nil && err != io.EOF {
		return fmt.Errorf("stdin read: %w", err)
	}
	return nil
}

func (s *Server) handle(req rpcRequest) {
	switch req.Method {
	case "initialize":
		s.writeResult(req.ID, map[string]interface{}{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
			"serverInfo":      map[string]string{"name": "universe", "version": "0.1.0"},
		})

	case "tools/list":
		s.writeResult(req.ID, map[string]interface{}{
			"tools": s.tools.Definitions(),
		})

	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			s.writeError(req.ID, -32602, "invalid params: "+err.Error())
			return
		}
		result, err := s.tools.Call(p.Name, p.Arguments)
		if err != nil {
			// MCP tool errors are returned as content, not as JSON-RPC errors
			s.writeResult(req.ID, toolErrorContent(p.Name, err))
			return
		}
		s.writeResult(req.ID, result)

	case "ping":
		s.writeResult(req.ID, map[string]interface{}{})

	default:
		s.writeError(req.ID, -32601, "method not found: "+req.Method)
	}
}

func (s *Server) writeResult(id interface{}, result interface{}) {
	s.write(rpcResponse{JSONRPC: "2.0", ID: id, Result: result})
}

func (s *Server) writeError(id interface{}, code int, msg string) {
	s.write(rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}})
}

func (s *Server) write(resp rpcResponse) {
	data, err := json.Marshal(resp)
	if err != nil {
		log.Printf("mcp: marshal response: %v", err)
		return
	}
	data = append(data, '\n')
	if _, err := s.out.Write(data); err != nil {
		log.Printf("mcp: write response: %v", err)
	}
}

// toolErrorContent wraps an error in MCP's content format so the agent
// sees a readable message rather than a JSON-RPC protocol error.
func toolErrorContent(name string, err error) map[string]interface{} {
	return map[string]interface{}{
		"content": []map[string]string{
			{"type": "text", "text": fmt.Sprintf("tool %s error: %s", name, err.Error())},
		},
		"isError": true,
	}
}

// TextContent wraps a string result in MCP's content format.
func TextContent(text string) map[string]interface{} {
	return map[string]interface{}{
		"content": []map[string]string{
			{"type": "text", "text": text},
		},
	}
}
