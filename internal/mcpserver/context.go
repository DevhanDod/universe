package mcpserver

// SessionContext manages per-session state for the MCP server.
//
// When Cursor connects, a new MCP session starts. On the first tool call
// that references a graph node, the session manager (Engine 2) is notified
// via OnToolCall. Subsequent recall_memory calls can then use those node IDs
// to automatically surface relevant observations.
//
// In the current implementation, session state is implicit: each tool handler
// that touches a graph node calls h.sessionMgr.OnToolCall(...) so Engine 2
// can accumulate context across calls within a session.
//
// No additional wiring is required here — the session manager is initialized
// in the CLI command and passed through ServerConfig.SessionMgr.
