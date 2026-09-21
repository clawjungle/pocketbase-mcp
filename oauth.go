package pbmcp

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

func randomClientID() string {
	return "mcp_" + security.RandomString(24)
}

// baseURL derives this server's externally-visible origin from the
// incoming request, honoring a reverse proxy's X-Forwarded-Proto (PocketBase
// itself sits behind Caddy in the deployments this package targets).
func baseURL(e *core.RequestEvent) string {
	scheme := "http"
	if proto := e.Request.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	} else if e.IsTLS() {
		scheme = "https"
	}
	return scheme + "://" + e.Request.Host
}

// --- well-known discovery -------------------------------------------------

func (s *Server) handleProtectedResourceMetadata(e *core.RequestEvent) error {
	base := baseURL(e)
	return e.JSON(http.StatusOK, map[string]any{
		"resource":              base + s.config.BasePath,
		"authorization_servers": []string{base},
	})
}

func (s *Server) handleAuthServerMetadata(e *core.RequestEvent) error {
	base := baseURL(e)
	return e.JSON(http.StatusOK, map[string]any{
		"issuer":                                     base,
		"authorization_endpoint":                     base + s.config.BasePath + "/oauth/authorize",
		"token_endpoint":                             base + s.config.BasePath + "/oauth/token",
		"registration_endpoint":                      base + s.config.BasePath + "/oauth/register",
		"response_types_supported":                   []string{"code"},
		"grant_types_supported":                      []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":           []string{"S256"},
		"token_endpoint_auth_methods_supported":      []string{"none"},
		"revocation_endpoint_auth_methods_supported": []string{"none"},
	})
}

// --- dynamic client registration (RFC 7591) -------------------------------

func (s *Server) handleRegister(e *core.RequestEvent) error {
	var body struct {
		ClientName   string   `json:"client_name"`
		RedirectURIs []string `json:"redirect_uris"`
	}
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("invalid registration request body", err)
	}
	if len(body.RedirectURIs) == 0 {
		return e.BadRequestError("redirect_uris is required", nil)
	}
	if body.ClientName == "" {
		body.ClientName = "MCP client"
	}

	client, err := createClient(e.App, body.ClientName, body.RedirectURIs)
	if err != nil {
		return e.InternalServerError("failed to register client", err)
	}

	return e.JSON(http.StatusCreated, map[string]any{
		"client_id":                  client.id,
		"client_id_issued_at":        time.Now().Unix(),
		"client_name":                client.name,
		"redirect_uris":              client.redirectURIs,
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
	})
}

// --- authorize (login form bridging to PocketBase auth) -------------------

func (s *Server) handleAuthorizeGet(e *core.RequestEvent) error {
	q := e.Request.URL.Query()
	if q.Get("response_type") != "code" {
		return e.BadRequestError("only response_type=code is supported", nil)
	}
	if q.Get("code_challenge_method") != "S256" {
		return e.BadRequestError("only code_challenge_method=S256 is supported", nil)
	}
	client, err := findClient(e.App, q.Get("client_id"))
	if err != nil {
		return e.BadRequestError("unknown client_id — register via /oauth/register first", nil)
	}
	if !client.allowsRedirect(q.Get("redirect_uri")) {
		return e.BadRequestError("redirect_uri does not match a registered redirect URI", nil)
	}

	return e.HTML(http.StatusOK, renderLoginForm(client.name, q, ""))
}

func (s *Server) handleAuthorizePost(e *core.RequestEvent) error {
	if err := e.Request.ParseForm(); err != nil {
		return e.BadRequestError("invalid form body", err)
	}
	f := e.Request.PostForm

	client, err := findClient(e.App, f.Get("client_id"))
	if err != nil {
		return e.BadRequestError("unknown client_id", nil)
	}
	if !client.allowsRedirect(f.Get("redirect_uri")) {
		return e.BadRequestError("redirect_uri does not match a registered redirect URI", nil)
	}

	record, err := e.App.FindAuthRecordByEmail(s.config.AuthCollection, f.Get("email"))
	if err != nil || !record.ValidatePassword(f.Get("password")) {
		q := url.Values{}
		for k, v := range f {
			if k != "email" && k != "password" {
				q[k] = v
			}
		}
		return e.HTML(http.StatusOK, renderLoginForm(client.name, q, "Incorrect email or password."))
	}

	code := &authCode{
		clientID:      f.Get("client_id"),
		redirectURI:   f.Get("redirect_uri"),
		codeChallenge: f.Get("code_challenge"),
		authRecordID:  record.Id,
		authCollName:  s.config.AuthCollection,
		expiresAt:     time.Now().Add(s.config.AuthCodeTTL),
	}
	s.codes.create(code)

	redirect := f.Get("redirect_uri") + "?code=" + url.QueryEscape(code.code)
	if state := f.Get("state"); state != "" {
		redirect += "&state=" + url.QueryEscape(state)
	}
	return e.Redirect(http.StatusFound, redirect)
}

func renderLoginForm(clientName string, q url.Values, errMsg string) string {
	var hidden strings.Builder
	for _, k := range []string{"response_type", "client_id", "redirect_uri", "state", "code_challenge", "code_challenge_method", "scope"} {
		if v := q.Get(k); v != "" {
			fmt.Fprintf(&hidden, `<input type="hidden" name="%s" value="%s">`, k, html.EscapeString(v))
		}
	}
	errHTML := ""
	if errMsg != "" {
		errHTML = `<p style="color:#c0392b">` + html.EscapeString(errMsg) + `</p>`
	}
	return `<!doctype html><html><head><meta charset="utf-8"><title>Sign in</title>
<meta name="viewport" content="width=device-width, initial-scale=1">
<style>
body{font:16px system-ui,sans-serif;max-width:360px;margin:15vh auto;padding:0 20px;color:#222}
input{width:100%;box-sizing:border-box;padding:10px;margin:6px 0 14px;font-size:16px;border:1px solid #ccc;border-radius:6px}
button{width:100%;padding:10px;font-size:16px;border:0;border-radius:6px;background:#111;color:#fff;cursor:pointer}
</style></head><body>
<h2>` + html.EscapeString(clientName) + ` wants to access your data</h2>
<p>Sign in with your account to continue.</p>
` + errHTML + `
<form method="post">
` + hidden.String() + `
<label>Email<input type="email" name="email" required autofocus></label>
<label>Password<input type="password" name="password" required></label>
<button type="submit">Sign in &amp; authorize</button>
</form>
</body></html>`
}

// --- token exchange ---------------------------------------------------

func (s *Server) handleToken(e *core.RequestEvent) error {
	if err := e.Request.ParseForm(); err != nil {
		return e.BadRequestError("invalid form body", err)
	}
	f := e.Request.PostForm

	switch f.Get("grant_type") {
	case "authorization_code":
		return s.exchangeAuthCode(e, f)
	case "refresh_token":
		return s.exchangeRefreshToken(e, f)
	default:
		return e.JSON(http.StatusBadRequest, map[string]any{
			"error":             "unsupported_grant_type",
			"error_description": "only authorization_code and refresh_token are supported",
		})
	}
}

func (s *Server) exchangeAuthCode(e *core.RequestEvent, f url.Values) error {
	code := s.codes.redeem(f.Get("code"))
	if code == nil {
		return e.JSON(http.StatusBadRequest, map[string]any{"error": "invalid_grant", "error_description": "unknown or expired code"})
	}
	if code.clientID != f.Get("client_id") || code.redirectURI != f.Get("redirect_uri") {
		return e.JSON(http.StatusBadRequest, map[string]any{"error": "invalid_grant", "error_description": "client_id/redirect_uri mismatch"})
	}
	if !verifyPKCE(code.codeChallenge, f.Get("code_verifier")) {
		return e.JSON(http.StatusBadRequest, map[string]any{"error": "invalid_grant", "error_description": "PKCE verification failed"})
	}

	record, err := e.App.FindRecordById(code.authCollName, code.authRecordID)
	if err != nil {
		return e.JSON(http.StatusBadRequest, map[string]any{"error": "invalid_grant", "error_description": "account no longer exists"})
	}

	return s.issueTokens(e, record)
}

func (s *Server) exchangeRefreshToken(e *core.RequestEvent, f url.Values) error {
	record, err := e.App.FindAuthRecordByToken(f.Get("refresh_token"), core.TokenTypeAuth)
	if err != nil {
		return e.JSON(http.StatusBadRequest, map[string]any{"error": "invalid_grant", "error_description": "invalid or expired refresh token"})
	}
	return s.issueTokens(e, record)
}

func (s *Server) issueTokens(e *core.RequestEvent, record *core.Record) error {
	access, err := record.NewStaticAuthToken(s.config.AccessTokenTTL)
	if err != nil {
		return e.InternalServerError("failed to mint access token", err)
	}
	refresh, err := record.NewStaticAuthToken(s.config.RefreshTokenTTL)
	if err != nil {
		return e.InternalServerError("failed to mint refresh token", err)
	}

	return e.JSON(http.StatusOK, map[string]any{
		"access_token":  access,
		"token_type":    "Bearer",
		"expires_in":    int(s.config.AccessTokenTTL.Seconds()),
		"refresh_token": refresh,
	})
}

func verifyPKCE(challenge, verifier string) bool {
	if challenge == "" || verifier == "" {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	computed := base64.RawURLEncoding.EncodeToString(sum[:])
	return computed == challenge
}
