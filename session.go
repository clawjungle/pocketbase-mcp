package pbmcp

import (
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/tools/security"
)

// mcpSession is the state kept for one Streamable HTTP session, identified
// by the Mcp-Session-Id header the server hands back from "initialize".
// Sessions are process-local and in-memory: a restart drops them, but a
// client just re-initializes, which the transport already handles fine.
type mcpSession struct {
	id        string
	createdAt time.Time
}

type sessionStore struct {
	mu   sync.Mutex
	byID map[string]*mcpSession
}

func newSessionStore() *sessionStore {
	return &sessionStore{byID: map[string]*mcpSession{}}
}

func (s *sessionStore) create() *mcpSession {
	sess := &mcpSession{id: security.RandomString(32), createdAt: time.Now()}
	s.mu.Lock()
	s.byID[sess.id] = sess
	s.mu.Unlock()
	return sess
}

func (s *sessionStore) get(id string) *mcpSession {
	if id == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.byID[id]
}

func (s *sessionStore) delete(id string) {
	s.mu.Lock()
	delete(s.byID, id)
	s.mu.Unlock()
}

// authCode is a one-time, short-lived OAuth authorization code minted at
// the end of the /authorize login form and redeemed by /token. It's the
// only piece of OAuth state this package keeps outside PocketBase's own
// record store — codes live seconds, so in-memory is the right tradeoff
// (see README for the durability note).
type authCode struct {
	code          string
	clientID      string
	redirectURI   string
	codeChallenge string
	authRecordID  string
	authCollName  string
	expiresAt     time.Time
}

type authCodeStore struct {
	mu   sync.Mutex
	byID map[string]*authCode
}

func newAuthCodeStore() *authCodeStore {
	return &authCodeStore{byID: map[string]*authCode{}}
}

func (s *authCodeStore) create(c *authCode) {
	c.code = security.RandomString(43) // matches typical OAuth code entropy
	s.mu.Lock()
	s.byID[c.code] = c
	s.mu.Unlock()
}

// redeem returns and deletes the code if it exists and hasn't expired —
// codes are single-use per RFC 6749 §4.1.2.
func (s *authCodeStore) redeem(code string) *authCode {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.byID[code]
	if !ok {
		return nil
	}
	delete(s.byID, code)
	if time.Now().After(c.expiresAt) {
		return nil
	}
	return c
}
