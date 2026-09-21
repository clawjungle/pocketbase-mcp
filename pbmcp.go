// Package pbmcp bakes an MCP (Model Context Protocol) server into a
// PocketBase app: a Streamable HTTP transport at a configurable path
// (default "/mcp"), an OAuth 2.1 + PKCE authorization layer bridged onto
// PocketBase's own auth collections and token format, and a Go-side
// AddTool API so the host app registers its own tools instead of this
// package exposing generic collection CRUD.
//
// Usage:
//
//	app := pocketbase.New()
//	srv := pbmcp.Register(app, pbmcp.Config{ServerName: "myapp"})
//	srv.AddTool(pbmcp.Tool{
//	    Name:        "echo",
//	    Description: "Echo back the given text",
//	    InputSchema: map[string]any{
//	        "type":       "object",
//	        "properties": map[string]any{"text": map[string]any{"type": "string"}},
//	        "required":   []string{"text"},
//	    },
//	    Handler: func(e *pbmcp.ToolEvent) (any, error) {
//	        return map[string]any{"text": e.Args["text"]}, nil
//	    },
//	})
//	app.Start()
package pbmcp

import (
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// Config controls how the MCP server is mounted and how OAuth tokens are
// minted. All fields have workable zero-value defaults.
type Config struct {
	// BasePath is where the MCP JSON-RPC endpoint is mounted. Default "/mcp".
	BasePath string

	// AuthCollection is the PocketBase auth collection users authenticate
	// against via the OAuth authorize screen (e.g. "users", "_superusers").
	// Default "users".
	AuthCollection string

	// AccessTokenTTL is the lifetime of minted OAuth access tokens (which
	// are plain PocketBase static auth tokens). Default 1 hour.
	AccessTokenTTL time.Duration

	// RefreshTokenTTL is the lifetime of minted OAuth refresh tokens
	// (also static auth tokens, just longer-lived). Default 30 days.
	RefreshTokenTTL time.Duration

	// AuthCodeTTL is how long an authorization code stays redeemable.
	// Default 60 seconds.
	AuthCodeTTL time.Duration

	// ServerName / ServerVersion identify this server in the MCP
	// "initialize" response and in generated OAuth client metadata.
	ServerName    string
	ServerVersion string
}

func (c Config) withDefaults() Config {
	if c.BasePath == "" {
		c.BasePath = "/mcp"
	}
	if c.AuthCollection == "" {
		c.AuthCollection = "users"
	}
	if c.AccessTokenTTL <= 0 {
		c.AccessTokenTTL = time.Hour
	}
	if c.RefreshTokenTTL <= 0 {
		c.RefreshTokenTTL = 30 * 24 * time.Hour
	}
	if c.AuthCodeTTL <= 0 {
		c.AuthCodeTTL = 60 * time.Second
	}
	if c.ServerName == "" {
		c.ServerName = "pocketbase-mcp"
	}
	if c.ServerVersion == "" {
		c.ServerVersion = "0.1.0"
	}
	return c
}

// Server holds the tool registry, session table and auth-code store for one
// mounted MCP endpoint. Returned by Register; keep it to call AddTool.
type Server struct {
	app    core.App
	config Config

	toolsMu sync.RWMutex
	tools   map[string]Tool
	order   []string // preserves registration order for tools/list

	sessions *sessionStore
	codes    *authCodeStore
}

// Register wires the MCP endpoint, OAuth endpoints and well-known discovery
// documents into app's router, and bootstraps the small internal
// collections the OAuth layer needs (client registrations). It's meant to
// be called once, before app.Start().
func Register(app core.App, config Config) *Server {
	cfg := config.withDefaults()

	s := &Server{
		app:      app,
		config:   cfg,
		tools:    map[string]Tool{},
		sessions: newSessionStore(),
		codes:    newAuthCodeStore(),
	}

	app.OnBootstrap().BindFunc(func(e *core.BootstrapEvent) error {
		if err := e.Next(); err != nil {
			return err
		}
		return bootstrapClientsCollection(e.App)
	})

	app.OnServe().BindFunc(func(e *core.ServeEvent) error {
		s.bindRoutes(e)
		return e.Next()
	})

	return s
}

// Tool is one callable MCP tool. InputSchema is the JSON Schema describing
// the "arguments" object tools/call is expected to send — usually a
// map[string]any built by hand (see package example), since Go has no
// native JSON Schema literal syntax.
type Tool struct {
	Name        string
	Description string
	InputSchema map[string]any

	// Handler runs the tool. Returning an error surfaces to the MCP client
	// as a tool-level failure (isError: true in the result), not a
	// transport-level JSON-RPC error — per spec, only malformed
	// requests/unknown methods are protocol errors.
	Handler func(e *ToolEvent) (any, error)
}

// ToolEvent is what a Tool.Handler receives.
type ToolEvent struct {
	App core.App

	// Auth is the PocketBase auth record behind the caller's bearer
	// token — the same record e.Auth would hold on any other PocketBase
	// route, resolved by PocketBase's own default auth-token middleware.
	Auth *core.Record

	// Args is the parsed "arguments" object from the tools/call request.
	Args map[string]any
}

// AddTool registers a tool. Returns the Server so calls can be chained.
// Registering two tools with the same Name replaces the earlier one.
func (s *Server) AddTool(t Tool) *Server {
	s.toolsMu.Lock()
	defer s.toolsMu.Unlock()
	if _, exists := s.tools[t.Name]; !exists {
		s.order = append(s.order, t.Name)
	}
	s.tools[t.Name] = t
	return s
}
