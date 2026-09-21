# pocketbase-mcp

Bakes an [MCP](https://modelcontextprotocol.io) (Model Context Protocol)
server into a [PocketBase](https://pocketbase.io) Go app, instead of running
a separate process that talks to PocketBase over its REST API — which is how
every other PocketBase MCP integration works today (see "Prior art" below).

- A Streamable HTTP transport at a configurable path (default `/mcp`).
- OAuth 2.1 + PKCE + Dynamic Client Registration, bridged directly onto
  PocketBase's own auth collections — no separate user database, no
  hand-rolled token format.
- A Go `AddTool` API so the host app registers its own tools. Nothing is
  exposed by default: no generic collection CRUD, no auto-discovered
  schema. You write tools the same way you'd write a PocketBase custom
  route.

## Why this shape

PocketBase itself has no MCP support and it isn't on the roadmap — see
[pocketbase/pocketbase#6134](https://github.com/pocketbase/pocketbase/discussions/6134),
closed unanswered. Every third-party PocketBase MCP server found in the wild
(`imiborbas/pocketbase-mcp-server`, `mabeldata/pocketbase-mcp`, the PyPI
`pocketbase-mcp`, etc.) is a standalone Node/Python process, launched
separately, authenticating with a static admin token or admin
email/password, exposing a fixed set of generic record-CRUD tools. None of
them are an importable Go plugin, none implement OAuth, and none let the
app developer define their own tools.

This package takes the opposite shape on purpose:

- **It's Go code you import**, not a process you run next to PocketBase.
  `pbmcp.Register(app, config)` is one call in `main.go`, the same way you'd
  wire up any other PocketBase plugin.
- **OAuth reuses PocketBase's own auth, not a parallel one.** The
  `/authorize` screen is a plain login form that calls
  `record.ValidatePassword` against a configured auth collection (`users` by
  default). Access and refresh tokens handed to MCP clients are literally
  `record.NewStaticAuthToken(ttl)` — the same static-token mechanism
  PocketBase already has — so a tool handler's `e.Auth` is populated by
  PocketBase's own default auth-token middleware, for free, and a normal
  PocketBase login token also just works as an MCP bearer token with no
  extra plumbing.
- **The tool surface starts empty.** You decide what an AI agent can do to
  your app; this package doesn't guess.

## Usage

```go
app := pocketbase.New()

srv := pbmcp.Register(app, pbmcp.Config{
    ServerName:    "myapp",
    ServerVersion: "0.1.0",
    // AuthCollection: "users" (default) — which PocketBase auth
    // collection the OAuth login screen checks credentials against.
})

srv.AddTool(pbmcp.Tool{
    Name:        "echo",
    Description: "Echo back the given text",
    InputSchema: map[string]any{
        "type":       "object",
        "properties": map[string]any{"text": map[string]any{"type": "string"}},
        "required":   []string{"text"},
    },
    Handler: func(e *pbmcp.ToolEvent) (any, error) {
        // e.App is the PocketBase app, e.Auth is the caller's resolved
        // auth record (nil is impossible here — the transport already
        // requires it), e.Args is the parsed "arguments" object.
        return map[string]any{"text": e.Args["text"]}, nil
    },
})

app.Start()
```

See `example/main.go` for a runnable demo app with two tools (`echo`,
`whoami`). Run it with:

```sh
go run ./example serve --http=127.0.0.1:8090
```

## What gets mounted

| Path | What |
|---|---|
| `POST /mcp` | JSON-RPC 2.0 messages (`initialize`, `tools/list`, `tools/call`, `ping`, `notifications/initialized`) |
| `GET /mcp` | `405` — no standalone server-push SSE stream (optional per spec; not implemented) |
| `DELETE /mcp` | Ends a session (`Mcp-Session-Id` header) |
| `GET /.well-known/oauth-protected-resource` (+ `/mcp` suffix variant) | RFC 9728 metadata pointing clients at the authorization server |
| `GET /.well-known/oauth-authorization-server` | RFC 8414 metadata (endpoints, supported grants, PKCE) |
| `POST /mcp/oauth/register` | RFC 7591 Dynamic Client Registration — persists a client into the `_mcpClients` collection |
| `GET`/`POST /mcp/oauth/authorize` | Login form (email/password against `AuthCollection`) → redirect with an authorization code |
| `POST /mcp/oauth/token` | `authorization_code` and `refresh_token` grants, PKCE-verified |

An unauthenticated request to `/mcp` gets `401` with a `WWW-Authenticate`
header pointing at the protected-resource metadata, per the MCP
authorization spec, which is what drives a compliant client into the OAuth
flow automatically.

## Known limitations (by design, for a first pass)

- **No standalone SSE / server-initiated pushes.** Every `tools/call`
  answers with a single JSON response. Fine for request/response tools;
  not for a tool that wants to stream partial results or push unsolicited
  notifications.
- **Sessions are in-memory**, not persisted. A restart drops active
  `Mcp-Session-Id`s; a client just re-`initialize`s, which is within spec.
- **Authorization codes are in-memory** (60s TTL) rather than a persisted
  collection — they're single-use and short-lived enough that this is the
  same tradeoff most OAuth servers make (fast ephemeral storage, not a
  durable table). OAuth *clients* (from Dynamic Client Registration) and
  the *access/refresh tokens* themselves are the parts that must survive a
  restart, and both do — clients as real PocketBase records, tokens as
  PocketBase's own signed static auth tokens.
- **Public clients only** (`token_endpoint_auth_method: none`), which
  matches how MCP clients that do Dynamic Client Registration behave in
  practice — there's no persistent client secret to leak.
- **No JSVM (`pb_hooks/*.pb.js`) binding yet.** Tools are Go-only for now;
  PocketBase's JS hook runtime doesn't expose a public extension point for
  injecting new globals into the JSVM pool without patching PocketBase
  itself, so a `mcpTool(...)` binding for `.pb.js` files is a separate,
  larger piece of work, not attempted here.
- No `resources` or `prompts` MCP primitives — only `tools`, which is the
  one this package's target use case (turning existing app actions into
  agent-callable functions) actually needs.

## Status

Built and manually verified end-to-end against a live PocketBase instance:
Dynamic Client Registration, the full PKCE authorize → token exchange,
`initialize` → `tools/list` → `tools/call` (success and error paths),
`refresh_token` grant, and session teardown all work against real
PocketBase auth records. `go build ./...`, `go vet ./...` and `go test ./...`
are clean. Not yet used in a production app.
