package browser

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// MCP is the Model Context Protocol: newline-delimited JSON-RPC 2.0 over
// stdio. Stdout carries protocol messages only; diagnostics go to stderr.
const (
	mcpProtocolVersion = "2025-06-18"
	mcpToolTimeout     = 90 * time.Second
)

var mcpVersions = map[string]bool{
	"2024-11-05": true,
	"2025-03-26": true,
	"2025-06-18": true,
	"2025-11-25": true,
}

type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type mcpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type mcpResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *mcpError       `json:"error,omitempty"`
}

type mcpContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type mcpToolResult struct {
	Content []mcpContent `json:"content"`
	IsError bool         `json:"isError,omitempty"`
}

type mcpServer struct {
	a *Automation
}

// RunMCP serves MCP requests from in and writes responses to out.
func RunMCP(a *Automation, in io.Reader, out, errOut io.Writer) int {
	s := &mcpServer{a: a}
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 64*1024), 8<<20)
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)

	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var req mcpRequest
		if err := json.Unmarshal(line, &req); err != nil {
			_ = enc.Encode(errorResponse(nil, -32700, "parse error: "+err.Error()))
			continue
		}
		if len(req.ID) == 0 || string(req.ID) == "null" {
			// Notifications (initialized, cancelled, …) need no response.
			continue
		}
		if resp := s.handle(req); resp != nil {
			if err := enc.Encode(resp); err != nil {
				fmt.Fprintf(errOut, "mob3 mcp: write: %v\n", err)
				return 1
			}
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(errOut, "mob3 mcp: read: %v\n", err)
		return 1
	}
	return 0
}

func (s *mcpServer) handle(req mcpRequest) *mcpResponse {
	switch req.Method {
	case "initialize":
		return s.initialize(req)
	case "ping":
		return successResponse(req.ID, map[string]any{})
	case "tools/list":
		return successResponse(req.ID, map[string]any{"tools": mcpToolDefinitions()})
	case "tools/call":
		return s.callTool(req)
	case "notifications/initialized", "notifications/cancelled", "logging/setLevel":
		return successResponse(req.ID, map[string]any{})
	default:
		return errorResponse(req.ID, -32601, "method not found: "+req.Method)
	}
}

func (s *mcpServer) initialize(req mcpRequest) *mcpResponse {
	var p struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	_ = json.Unmarshal(req.Params, &p)
	version := p.ProtocolVersion
	if !mcpVersions[version] {
		version = mcpProtocolVersion
	}
	return successResponse(req.ID, map[string]any{
		"protocolVersion": version,
		"capabilities": map[string]any{
			"tools": map[string]any{},
		},
		"serverInfo": map[string]any{
			"name":    "mob3",
			"version": Version,
		},
		"instructions": "MOB³ is a terminal browser. Use snapshot for readable page text, " +
			"links as [n], and form-control positions; click accepts link, selector, x/y. " +
			"Use evaluate for DOM queries. Sessions and storage are memory-only.",
	})
}

func (s *mcpServer) callTool(req mcpRequest) *mcpResponse {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errorResponse(req.ID, -32602, "invalid params: "+err.Error())
	}
	ctx, cancel := context.WithTimeout(context.Background(), mcpToolTimeout)
	defer cancel()
	text, err := s.a.RunCommand(ctx, p.Name, p.Arguments)
	if err != nil {
		return successResponse(req.ID, mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: err.Error()}},
			IsError: true,
		})
	}
	return successResponse(req.ID, mcpToolResult{
		Content: []mcpContent{{Type: "text", Text: clipText(text, 20000)}},
	})
}

func successResponse(id json.RawMessage, result any) *mcpResponse {
	if id == nil {
		id = json.RawMessage("null")
	}
	return &mcpResponse{JSONRPC: "2.0", ID: id, Result: result}
}

func errorResponse(id json.RawMessage, code int, message string) *mcpResponse {
	if id == nil {
		id = json.RawMessage("null")
	}
	return &mcpResponse{JSONRPC: "2.0", ID: id, Error: &mcpError{Code: code, Message: message}}
}

func mcpToolDefinitions() []map[string]any {
	object := func(props map[string]any, required ...string) map[string]any {
		m := map[string]any{"type": "object", "properties": props}
		if len(required) > 0 {
			m["required"] = required
		}
		return m
	}
	str := func(desc string) map[string]any {
		return map[string]any{"type": "string", "description": desc}
	}
	integer := func(desc string) map[string]any {
		return map[string]any{"type": "integer", "description": desc}
	}
	boolean := func(desc string) map[string]any {
		return map[string]any{"type": "boolean", "description": desc}
	}
	return []map[string]any{
		{
			"name":        "navigate",
			"description": "Open a URL, wait for it to load, and return a snapshot of the page.",
			"inputSchema": object(map[string]any{"url": str("URL or search query")}, "url"),
		},
		{
			"name":        "snapshot",
			"description": "Return the current page text, visible numbered links and form controls with coordinates.",
			"inputSchema": object(map[string]any{}),
		},
		{
			"name": "click",
			"description": "Click a numbered link, a CSS selector, or a zero-based viewport cell. " +
				"Returns a fresh snapshot after any navigation.",
			"inputSchema": object(map[string]any{
				"link":     integer("1-based link number from snapshot"),
				"selector": str("CSS selector"),
				"x":        integer("zero-based page column"),
				"y":        integer("zero-based page row"),
			}),
		},
		{
			"name":        "type",
			"description": "Type text into the focused input, textarea or URL prompt.",
			"inputSchema": object(map[string]any{"text": str("text to type")}, "text"),
		},
		{
			"name":        "press",
			"description": "Press one key: Enter, Tab, Escape, Backspace, Delete, arrows, Home, End, PageUp, PageDown, Space.",
			"inputSchema": object(map[string]any{"key": str("key name")}, "key"),
		},
		{
			"name":        "scroll",
			"description": "Scroll the page by lines or pages, or jump to top/bottom.",
			"inputSchema": object(map[string]any{
				"lines":  integer("line delta"),
				"pages":  integer("page delta"),
				"top":    boolean("jump to top"),
				"bottom": boolean("jump to bottom"),
			}),
		},
		{
			"name":        "back",
			"description": "Go back in session history and return a snapshot.",
			"inputSchema": object(map[string]any{}),
		},
		{
			"name":        "forward",
			"description": "Go forward in session history and return a snapshot.",
			"inputSchema": object(map[string]any{}),
		},
		{
			"name":        "reload",
			"description": "Reload the current page and return a snapshot.",
			"inputSchema": object(map[string]any{}),
		},
		{
			"name":        "evaluate",
			"description": "Evaluate JavaScript in the page and return the result as text. Use JSON.stringify for objects.",
			"inputSchema": object(map[string]any{"expression": str("JavaScript expression")}, "expression"),
		},
		{
			"name":        "history",
			"description": "List session history, most recent first; * marks the current entry.",
			"inputSchema": object(map[string]any{}),
		},
	}
}
