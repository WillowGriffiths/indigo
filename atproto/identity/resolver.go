package identity

import (
	"context"
	"encoding/json"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

// Low-level interface for resolving DIDs and atproto handles.
//
// Most atproto code should use the `identity.Directory` interface instead.
type Resolver interface {
	ResolveDID(ctx context.Context, did syntax.DID) (*DIDDocument, error)
	ResolveDIDRaw(ctx context.Context, did syntax.DID) (json.RawMessage, error)
	ResolveHandle(ctx context.Context, handle syntax.Handle) (syntax.DID, error)
}

// Minimal interface for resolving an atproto handle (hostname) to a DID.
//
// This is a subset of the [Resolver] interface, and is useful for configuring
// alternate handle resolution behavior (eg, routing all handle resolution
// through a remote service) without providing DID resolution.
//
// [BaseDirectory] can be configured with a [HandleResolver] via its
// `HandleResolver` field.
type HandleResolver interface {
	ResolveHandle(ctx context.Context, handle syntax.Handle) (syntax.DID, error)
}
