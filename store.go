package pbmcp

import (
	"github.com/pocketbase/pocketbase/core"
)

// mcpClientsCollection is the one piece of OAuth state this package
// persists as a real PocketBase collection rather than in-memory: client
// registrations must survive a restart, or every previously-connected MCP
// client would silently break. It has no API rules set (nil = superuser
// only), so it's invisible to the normal REST API — only this package's
// Go code touches it directly.
const mcpClientsCollection = "_mcpClients"

// bootstrapClientsCollection creates the _mcpClients collection on first
// run. Safe to call on every boot — it's a no-op once the collection
// exists.
func bootstrapClientsCollection(app core.App) error {
	if _, err := app.FindCollectionByNameOrId(mcpClientsCollection); err == nil {
		return nil // already there
	}

	col := core.NewBaseCollection(mcpClientsCollection)
	col.System = true

	col.Fields.Add(&core.TextField{
		Name:     "client_id",
		System:   true,
		Required: true,
	})
	col.Fields.Add(&core.TextField{
		Name:   "client_name",
		System: true,
	})
	col.Fields.Add(&core.JSONField{
		Name:     "redirect_uris",
		System:   true,
		Required: true,
	})
	col.Fields.Add(&core.AutodateField{
		Name:     "created",
		System:   true,
		OnCreate: true,
	})

	col.AddIndex("idx_mcpClients_client_id", true, "client_id", "")

	return app.Save(col)
}

// oauthClient is the subset of a registered client this package needs at
// authorize/token time.
type oauthClient struct {
	id           string
	name         string
	redirectURIs []string
}

func createClient(app core.App, name string, redirectURIs []string) (*oauthClient, error) {
	col, err := app.FindCollectionByNameOrId(mcpClientsCollection)
	if err != nil {
		return nil, err
	}

	rec := core.NewRecord(col)
	clientID := randomClientID()
	rec.Set("client_id", clientID)
	rec.Set("client_name", name)
	rec.Set("redirect_uris", redirectURIs)

	if err := app.Save(rec); err != nil {
		return nil, err
	}

	return &oauthClient{id: clientID, name: name, redirectURIs: redirectURIs}, nil
}

func findClient(app core.App, clientID string) (*oauthClient, error) {
	rec, err := app.FindFirstRecordByFilter(mcpClientsCollection, "client_id = {:id}", map[string]any{"id": clientID})
	if err != nil {
		return nil, err
	}
	return &oauthClient{
		id:           rec.GetString("client_id"),
		name:         rec.GetString("client_name"),
		redirectURIs: rec.GetStringSlice("redirect_uris"),
	}, nil
}

func (c *oauthClient) allowsRedirect(uri string) bool {
	for _, r := range c.redirectURIs {
		if r == uri {
			return true
		}
	}
	return false
}
