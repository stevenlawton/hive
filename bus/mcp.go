package bus

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// ServeMCP runs a minimal MCP (Model Context Protocol) stdio server that
// exposes the Hive bus as native Claude Code tools. When Claude Code has
// this server configured, its tool inventory gains hive_bus_intent,
// hive_bus_waiting, hive_bus_done, hive_bus_ask, hive_bus_reply, and
// hive_bus_list alongside the built-in Bash/Read/Edit/etc.
//
// Protocol: newline-delimited JSON-RPC 2.0 over stdin/stdout.
func ServeMCP() error {
	b, err := Open(DetectSender())
	if err != nil {
		return fmt.Errorf("bus open: %w", err)
	}

	reader := bufio.NewReader(os.Stdin)
	writer := bufio.NewWriter(os.Stdout)
	defer writer.Flush()
	enc := json.NewEncoder(writer)

	for {
		line, err := reader.ReadBytes('\n')
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		line = bytesTrim(line)
		if len(line) == 0 {
			continue
		}

		var req jsonRPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			continue
		}

		resp := handleRequest(b, &req)
		if resp == nil {
			// notification — no response expected
			continue
		}
		if err := enc.Encode(resp); err != nil {
			return err
		}
		if err := writer.Flush(); err != nil {
			return err
		}
	}
}

// --- JSON-RPC framing ---

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func rpcOK(id json.RawMessage, result any) *jsonRPCResponse {
	return &jsonRPCResponse{JSONRPC: "2.0", ID: id, Result: result}
}

func rpcErr(id json.RawMessage, code int, msg string) *jsonRPCResponse {
	return &jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &jsonRPCError{Code: code, Message: msg},
	}
}

// --- Request dispatch ---

func handleRequest(b *Bus, req *jsonRPCRequest) *jsonRPCResponse {
	switch req.Method {
	case "initialize":
		return rpcOK(req.ID, initializeResult())
	case "notifications/initialized":
		return nil
	case "tools/list":
		return rpcOK(req.ID, map[string]any{"tools": toolDefinitions()})
	case "tools/call":
		return handleToolCall(b, req)
	case "ping":
		return rpcOK(req.ID, map[string]any{})
	case "shutdown":
		return rpcOK(req.ID, nil)
	}
	return rpcErr(req.ID, -32601, "method not found: "+req.Method)
}

func initializeResult() map[string]any {
	return map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities": map[string]any{
			"tools": map[string]any{},
		},
		"serverInfo": map[string]any{
			"name":    "hive-bus",
			"version": "1.0.0",
		},
	}
}

// --- Tool definitions ---

// toolDefinitions returns the list of tools this server exposes. Each
// tool is one narrow action — separate `hive_bus_intent`, `hive_bus_done`
// etc. rather than a single `hive_bus_announce` with a kind enum —
// because LLMs pick up specific named tools more reliably than generic
// tools with discriminator parameters.
func toolDefinitions() []map[string]any {
	return []map[string]any{
		announceTool(
			"hive_bus_intent",
			"Announce on the Hive bus that you are about to work on something. Use this BEFORE making changes to shared code, interfaces, or types so peer Claude sessions in other worktrees can flag conflicts or offer context. Short headlines are best — one sentence on what you're about to touch.",
		),
		announceTool(
			"hive_bus_waiting",
			"Announce on the Hive bus that you are blocked waiting on something — a code review, another worktree's change, a deployment, etc. Peer sessions can see what you're waiting on and chime in if they can unblock you.",
		),
		announceTool(
			"hive_bus_done",
			"Announce on the Hive bus that you have finished something. Use this after committing a significant change, completing a task, or resolving a blocker so peers know the work is settled and they can plan around it.",
		),
		announceTool(
			"hive_bus_ask",
			"Ask peer Claude sessions on the Hive bus for information you don't have — 'anyone using X?', 'who owns Y?', 'is Z still valid?'. Peers in other worktrees may have the answer from work they're doing. Replies arrive in your inbox digest on your next turn.",
		),
		{
			"name":        "hive_bus_reply",
			"description": "Reply to a specific existing bus message by its id. Use when you saw a peer's announcement or question in your inbox digest and have something useful to tell them. Keeps the conversation threaded.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"msg_id": map[string]any{
						"type":        "string",
						"description": "The id of the message to reply to (e.g. msg_abc123).",
					},
					"text": map[string]any{
						"type":        "string",
						"description": "Your reply.",
					},
					"body": map[string]any{
						"type":        "string",
						"description": "Optional extended body with more context.",
					},
				},
				"required": []string{"msg_id", "text"},
			},
		},
		{
			"name":        "hive_bus_list",
			"description": "List recent messages on the Hive bus. Use when you want to see the full recent chatter — the inbox digest only shows unread messages, but this shows everything. Returns headline, sender, kind, and id so you can hive_bus_read <id> for details or hive_bus_reply <id> to thread.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"n": map[string]any{
						"type":        "integer",
						"description": "How many recent messages to show (default 20).",
					},
				},
			},
		},
		{
			"name":        "hive_bus_read",
			"description": "Read the full body of one bus message by its id. Use when a headline in the inbox digest or hive_bus_list caught your attention and you want the full details before deciding how to respond.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{
						"type":        "string",
						"description": "The message id (e.g. msg_abc123).",
					},
				},
				"required": []string{"id"},
			},
		},
	}
}

// announceTool is a helper for building the three lifecycle announce
// tools (intent/waiting/done) that share the same input schema.
func announceTool(name, description string) map[string]any {
	return map[string]any{
		"name":        name,
		"description": description,
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"headline": map[string]any{
					"type":        "string",
					"description": "Short one-line summary (80-120 chars).",
				},
				"body": map[string]any{
					"type":        "string",
					"description": "Optional extended body with more context.",
				},
				"touches": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Optional file globs your work affects (e.g. services/auth/*).",
				},
			},
			"required": []string{"headline"},
		},
	}
}

// --- Tool invocation ---

type callParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type announceArgs struct {
	Headline string   `json:"headline"`
	Body     string   `json:"body"`
	Touches  []string `json:"touches"`
}

type replyArgs struct {
	MsgID string `json:"msg_id"`
	Text  string `json:"text"`
	Body  string `json:"body"`
}

type listArgs struct {
	N int `json:"n"`
}

type readArgs struct {
	ID string `json:"id"`
}

func handleToolCall(b *Bus, req *jsonRPCRequest) *jsonRPCResponse {
	var p callParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return rpcErr(req.ID, -32602, "invalid params: "+err.Error())
	}

	switch p.Name {
	case "hive_bus_intent":
		return callAnnounce(b, req.ID, p.Arguments, KindIntent)
	case "hive_bus_waiting":
		return callAnnounce(b, req.ID, p.Arguments, KindWaiting)
	case "hive_bus_done":
		return callAnnounce(b, req.ID, p.Arguments, KindDone)
	case "hive_bus_ask":
		return callAnnounce(b, req.ID, p.Arguments, KindQuestion)
	case "hive_bus_reply":
		return callReply(b, req.ID, p.Arguments)
	case "hive_bus_list":
		return callList(b, req.ID, p.Arguments)
	case "hive_bus_read":
		return callRead(b, req.ID, p.Arguments)
	}
	return rpcErr(req.ID, -32601, "unknown tool: "+p.Name)
}

func callAnnounce(b *Bus, id json.RawMessage, raw json.RawMessage, kind Kind) *jsonRPCResponse {
	var a announceArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return toolError(id, "invalid arguments: "+err.Error())
	}
	a.Headline = strings.TrimSpace(a.Headline)
	if a.Headline == "" {
		return toolError(id, "headline is required")
	}
	msg, err := b.Announce(Announcement{
		Kind:     kind,
		Headline: a.Headline,
		Body:     a.Body,
		Touches:  a.Touches,
	})
	if err != nil {
		return toolError(id, "announce failed: "+err.Error())
	}
	return toolText(id, fmt.Sprintf("Posted %s — message id: %s", kind, msg.ID))
}

func callReply(b *Bus, id json.RawMessage, raw json.RawMessage) *jsonRPCResponse {
	var a replyArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return toolError(id, "invalid arguments: "+err.Error())
	}
	a.MsgID = strings.TrimSpace(a.MsgID)
	a.Text = strings.TrimSpace(a.Text)
	if a.MsgID == "" || a.Text == "" {
		return toolError(id, "msg_id and text are required")
	}
	msg, err := b.Announce(Announcement{
		Headline: a.Text,
		Body:     a.Body,
		ReplyTo:  a.MsgID,
	})
	if err != nil {
		return toolError(id, "reply failed: "+err.Error())
	}
	return toolText(id, fmt.Sprintf("Reply posted — message id: %s", msg.ID))
}

func callList(b *Bus, id json.RawMessage, raw json.RawMessage) *jsonRPCResponse {
	a := listArgs{N: 20}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &a)
	}
	if a.N <= 0 {
		a.N = 20
	}
	msgs := b.Tail(a.N)
	if len(msgs) == 0 {
		return toolText(id, "(no messages on the bus)")
	}
	var sb strings.Builder
	for _, m := range msgs {
		age := m.At.Format("15:04")
		fmt.Fprintf(&sb, "[%s] %s · %s · %s %s\n", m.ID, age, m.From, m.Icon(), m.Headline)
		if m.ReplyTo != "" {
			fmt.Fprintf(&sb, "      ↳ reply to %s\n", m.ReplyTo)
		}
		if len(m.Touches) > 0 {
			fmt.Fprintf(&sb, "      touches: %s\n", strings.Join(m.Touches, ", "))
		}
	}
	return toolText(id, strings.TrimRight(sb.String(), "\n"))
}

func callRead(b *Bus, id json.RawMessage, raw json.RawMessage) *jsonRPCResponse {
	var a readArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return toolError(id, "invalid arguments: "+err.Error())
	}
	a.ID = strings.TrimSpace(a.ID)
	if a.ID == "" {
		return toolError(id, "id is required")
	}
	msg := b.Find(a.ID)
	if msg == nil {
		return toolError(id, "message not found: "+a.ID)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "id:       %s\n", msg.ID)
	fmt.Fprintf(&sb, "from:     %s\n", msg.From)
	fmt.Fprintf(&sb, "at:       %s\n", msg.At.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&sb, "kind:     %s\n", msg.KindOrDefault())
	if msg.ReplyTo != "" {
		fmt.Fprintf(&sb, "reply_to: %s\n", msg.ReplyTo)
	}
	fmt.Fprintf(&sb, "headline: %s\n", msg.Headline)
	if len(msg.Touches) > 0 {
		fmt.Fprintf(&sb, "touches:  %s\n", strings.Join(msg.Touches, ", "))
	}
	if msg.Body != "" {
		sb.WriteString("\n")
		sb.WriteString(msg.Body)
	}
	return toolText(id, sb.String())
}

// --- Response helpers ---

func toolText(id json.RawMessage, text string) *jsonRPCResponse {
	return rpcOK(id, map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": text},
		},
	})
}

func toolError(id json.RawMessage, text string) *jsonRPCResponse {
	return rpcOK(id, map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": text},
		},
		"isError": true,
	})
}

// bytesTrim strips trailing whitespace/newlines from a JSON-RPC line.
// Done inline rather than importing strings just for TrimRight.
func bytesTrim(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r' || b[len(b)-1] == ' ' || b[len(b)-1] == '\t') {
		b = b[:len(b)-1]
	}
	return b
}
