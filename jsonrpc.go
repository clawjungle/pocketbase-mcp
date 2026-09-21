package pbmcp

import "encoding/json"

// JSON-RPC 2.0 envelope, per https://www.jsonrpc.org/specification and the
// MCP spec's use of it (https://modelcontextprotocol.io).

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`

	// Present only on responses (never sent by a client to this server,
	// but reused for building outgoing messages below).
	Result any       `json:"result,omitempty"`
	Error  *rpcError `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// isNotification reports whether a request carries no id, meaning the
// caller doesn't want (and mustn't receive) a response.
func (m rpcMessage) isNotification() bool {
	return len(m.ID) == 0
}

func rpcResult(id json.RawMessage, result any) rpcMessage {
	return rpcMessage{JSONRPC: "2.0", ID: id, Result: result}
}

func rpcErrorResponse(id json.RawMessage, code int, message string, data any) rpcMessage {
	return rpcMessage{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: message, Data: data}}
}

// Standard JSON-RPC error codes used by this server.
const (
	rpcParseError     = -32700
	rpcInvalidRequest = -32600
	rpcMethodNotFound = -32601
	rpcInvalidParams  = -32602
	rpcInternalError  = -32603
)
