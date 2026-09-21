// Command example runs a bare PocketBase app with pbmcp mounted at /mcp and
// two demo tools, to exercise the whole stack end to end: OAuth discovery,
// dynamic client registration, the login-form authorize flow, PKCE token
// exchange, and a couple of tools/call round trips.
//
//	go run ./example serve --http=127.0.0.1:8090
package main

import (
	"fmt"
	"log"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"

	"github.com/ponelat/pocketbase-mcp"
)

func main() {
	app := pocketbase.New()

	srv := pbmcp.Register(app, pbmcp.Config{
		ServerName:    "pocketbase-mcp-example",
		ServerVersion: "0.1.0",
	})

	srv.AddTool(pbmcp.Tool{
		Name:        "echo",
		Description: "Echo back the given text",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text": map[string]any{"type": "string"},
			},
			"required": []string{"text"},
		},
		Handler: func(e *pbmcp.ToolEvent) (any, error) {
			return map[string]any{"text": e.Args["text"]}, nil
		},
	})

	srv.AddTool(pbmcp.Tool{
		Name:        "whoami",
		Description: "Return the PocketBase account the caller authenticated as",
		InputSchema: map[string]any{"type": "object"},
		Handler: func(e *pbmcp.ToolEvent) (any, error) {
			if e.Auth == nil {
				return nil, fmt.Errorf("no authenticated record on this request")
			}
			return map[string]any{
				"id":         e.Auth.Id,
				"collection": e.Auth.Collection().Name,
				"email":      e.Auth.GetString("email"),
			}, nil
		},
	})

	app.OnServe().BindFunc(func(e *core.ServeEvent) error {
		log.Println("MCP mounted at /mcp — discovery at /.well-known/oauth-protected-resource")
		return e.Next()
	})

	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}
