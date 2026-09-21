package pbmcp

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/pocketbase/pocketbase/core"
)

const protocolVersion = "2025-06-18"

func (s *Server) bindRoutes(e *core.ServeEvent) {
	base := s.config.BasePath

	e.Router.GET("/.well-known/oauth-protected-resource", s.handleProtectedResourceMetadata)
	e.Router.GET("/.well-known/oauth-protected-resource"+base, s.handleProtectedResourceMetadata)
	e.Router.GET("/.well-known/oauth-authorization-server", s.handleAuthServerMetadata)

	e.Router.GET(base+"/oauth/authorize", s.handleAuthorizeGet)
	e.Router.POST(base+"/oauth/authorize", s.handleAuthorizePost)
	e.Router.POST(base+"/oauth/token", s.handleToken)
	e.Router.POST(base+"/oauth/register", s.handleRegister)

	e.Router.POST(base, s.handleMCPPost)
	e.Router.GET(base, s.handleMCPGet)
	e.Router.DELETE(base, s.handleMCPDelete)
}

// requireAuth is the MCP-side counterpart of the OAuth "protected resource"
// dance: a request with no (or an invalid) bearer token gets a 401 that
// points the client at the discovery document, per the MCP authorization
// spec. e.Auth is already populated for us by PocketBase's own default
// auth-token middleware if the token is valid — see apis/middlewares.go
// (loadAuthToken), which runs on every route before ours.
func (s *Server) requireAuth(e *core.RequestEvent) bool {
	if e.Auth != nil {
		return true
	}
	e.Response.Header().Set("WWW-Authenticate",
		`Bearer resource_metadata="`+baseURL(e)+`/.well-known/oauth-protected-resource"`)
	_ = e.JSON(http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
	return false
}

func (s *Server) handleMCPGet(e *core.RequestEvent) error {
	if !s.requireAuth(e) {
		return nil
	}
	// Server-initiated SSE streaming (independent of a client request) is
	// optional per spec ("a server MAY support..."); this server doesn't.
	e.Response.Header().Set("Allow", "POST, DELETE")
	return e.JSON(http.StatusMethodNotAllowed, map[string]any{
		"error": "this server only supports request/response over POST; no standalone SSE stream",
	})
}

func (s *Server) handleMCPDelete(e *core.RequestEvent) error {
	if !s.requireAuth(e) {
		return nil
	}
	s.sessions.delete(e.Request.Header.Get("Mcp-Session-Id"))
	return e.NoContent(http.StatusNoContent)
}

func (s *Server) handleMCPPost(e *core.RequestEvent) error {
	if !s.requireAuth(e) {
		return nil
	}

	raw, err := readBody(e)
	if err != nil {
		return e.BadRequestError("failed to read request body", err)
	}

	var batch []rpcMessage
	if err := json.Unmarshal(raw, &batch); err != nil {
		// not a JSON array — try a single message
		var single rpcMessage
		if err := json.Unmarshal(raw, &single); err != nil {
			return e.JSON(http.StatusOK, rpcErrorResponse(nil, rpcParseError, "invalid JSON-RPC message", err.Error()))
		}
		batch = []rpcMessage{single}
	}

	responses := make([]rpcMessage, 0, len(batch))
	for _, msg := range batch {
		resp := s.dispatch(e, msg)
		if resp != nil {
			responses = append(responses, *resp)
		}
	}

	if len(responses) == 0 {
		// every message was a notification — no response body expected
		return e.NoContent(http.StatusAccepted)
	}
	if len(responses) == 1 && len(batch) == 1 {
		return e.JSON(http.StatusOK, responses[0])
	}
	return e.JSON(http.StatusOK, responses)
}

func readBody(e *core.RequestEvent) ([]byte, error) {
	return io.ReadAll(e.Request.Body)
}

// dispatch runs one JSON-RPC message and returns the response to send back,
// or nil if msg was a notification (no response expected).
func (s *Server) dispatch(e *core.RequestEvent, msg rpcMessage) *rpcMessage {
	var resp rpcMessage

	switch msg.Method {
	case "initialize":
		sess := s.sessions.create()
		e.Response.Header().Set("Mcp-Session-Id", sess.id)
		resp = rpcResult(msg.ID, map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": s.config.ServerName, "version": s.config.ServerVersion},
		})

	case "notifications/initialized":
		return nil

	case "ping":
		resp = rpcResult(msg.ID, map[string]any{})

	case "tools/list":
		resp = rpcResult(msg.ID, map[string]any{"tools": s.listToolDescriptors()})

	case "tools/call":
		var params toolCallParams
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			resp = rpcErrorResponse(msg.ID, rpcInvalidParams, "invalid tools/call params", err.Error())
			break
		}
		result, rpcErr := s.callTool(e.App, e.Auth, params)
		if rpcErr != nil {
			resp = rpcErrorResponse(msg.ID, rpcErr.Code, rpcErr.Message, rpcErr.Data)
			break
		}
		resp = rpcResult(msg.ID, result)

	default:
		resp = rpcErrorResponse(msg.ID, rpcMethodNotFound, "method not found: "+msg.Method, nil)
	}

	if msg.isNotification() {
		return nil
	}
	return &resp
}
