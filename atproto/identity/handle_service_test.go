package identity_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/identity/apidir"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/stretchr/testify/assert"
)

// Verifies that a BaseDirectory configured with a remote HandleResolver routes
// handle resolution through the `com.atproto.identity.resolveHandle` XRPC
// endpoint, using an apidir client (as tap does for dev environments).
func TestResolveHandleThroughService(t *testing.T) {
	assert := assert.New(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal("/xrpc/com.atproto.identity.resolveHandle", r.URL.Path)
		switch r.URL.Query().Get("handle") {
		case "alice.local":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"did": "did:plc:ewvi7nxzyoun6zhxrhs64oiz"})
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"HandleNotFound"}`))
		}
	}))
	defer srv.Close()

	dir := identity.BaseDirectory{
		HandleResolver: apidir.NewAPIDirectory(srv.URL),
	}

	// reserved-TLD handle is routed to the service (not rejected locally)
	did, err := dir.ResolveHandle(context.Background(), syntax.Handle("alice.local"))
	assert.NoError(err)
	assert.Equal(syntax.DID("did:plc:ewvi7nxzyoun6zhxrhs64oiz"), did)

	// unknown handle surfaces as ErrHandleNotFound
	_, err = dir.ResolveHandle(context.Background(), syntax.Handle("bob.local"))
	assert.ErrorIs(err, identity.ErrHandleNotFound)
}
