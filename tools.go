package pbmcp

import (
	"encoding/json"

	"github.com/pocketbase/pocketbase/core"
)

type toolDescriptor struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

func (s *Server) listToolDescriptors() []toolDescriptor {
	s.toolsMu.RLock()
	defer s.toolsMu.RUnlock()

	out := make([]toolDescriptor, 0, len(s.order))
	for _, name := range s.order {
		t := s.tools[name]
		schema := t.InputSchema
		if schema == nil {
			schema = map[string]any{"type": "object"}
		}
		out = append(out, toolDescriptor{Name: t.Name, Description: t.Description, InputSchema: schema})
	}
	return out
}

func (s *Server) getTool(name string) (Tool, bool) {
	s.toolsMu.RLock()
	defer s.toolsMu.RUnlock()
	t, ok := s.tools[name]
	return t, ok
}

type toolCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// callTool runs a tool and builds the MCP tools/call result. Execution
// errors (the Handler returning err) become isError:true results, per the
// MCP spec — they are not JSON-RPC protocol errors.
func (s *Server) callTool(app core.App, auth *core.Record, params toolCallParams) (any, *rpcError) {
	t, ok := s.getTool(params.Name)
	if !ok {
		return nil, &rpcError{Code: rpcInvalidParams, Message: "unknown tool: " + params.Name}
	}

	result, err := t.Handler(&ToolEvent{App: app, Auth: auth, Args: params.Arguments})
	if err != nil {
		return map[string]any{
			"content": []map[string]any{{"type": "text", "text": err.Error()}},
			"isError": true,
		}, nil
	}

	text := ""
	switch v := result.(type) {
	case string:
		text = v
	case nil:
		text = ""
	default:
		b, mErr := json.MarshalIndent(v, "", "  ")
		if mErr != nil {
			return map[string]any{
				"content": []map[string]any{{"type": "text", "text": "failed to encode tool result: " + mErr.Error()}},
				"isError": true,
			}, nil
		}
		text = string(b)
	}

	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
	}, nil
}
