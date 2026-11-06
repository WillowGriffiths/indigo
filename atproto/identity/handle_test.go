package identity

import (
	"context"
	"fmt"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/stretchr/testify/assert"
)

// stub implementation of the HandleResolver interface, for tests
type stubHandleResolver struct {
	did    syntax.DID
	err    error
	calls  int
	handle syntax.Handle
}

func (s *stubHandleResolver) ResolveHandle(ctx context.Context, handle syntax.Handle) (syntax.DID, error) {
	s.calls++
	s.handle = handle
	return s.did, s.err
}

func TestResolveHandleCustomResolver(t *testing.T) {
	assert := assert.New(t)
	ctx := context.Background()

	stub := &stubHandleResolver{did: syntax.DID("did:plc:ewvi7nxzyoun6zhxrhs64oiz")}
	dir := BaseDirectory{HandleResolver: stub}

	// resolution is delegated, and normal (reserved) TLD policy is bypassed so
	// that development handle domains can be used
	did, err := dir.ResolveHandle(ctx, syntax.Handle("alice.local"))
	assert.NoError(err)
	assert.Equal(syntax.DID("did:plc:ewvi7nxzyoun6zhxrhs64oiz"), did)
	assert.Equal(1, stub.calls)
	assert.Equal(syntax.Handle("alice.local"), stub.handle)

	// the special "handle.invalid" handle is rejected before delegation
	_, err = dir.ResolveHandle(ctx, syntax.HandleInvalid)
	assert.ErrorIs(err, ErrInvalidHandle)
	assert.Equal(1, stub.calls)

	// errors from the configured resolver are returned as-is
	stub.err = fmt.Errorf("%w: nope", ErrHandleNotFound)
	_, err = dir.ResolveHandle(ctx, syntax.Handle("bob.test"))
	assert.ErrorIs(err, ErrHandleNotFound)
	assert.Equal(2, stub.calls)

	// without a configured resolver, reserved TLDs are still rejected locally
	base := BaseDirectory{}
	_, err = base.ResolveHandle(ctx, syntax.Handle("alice.local"))
	assert.ErrorIs(err, ErrHandleReservedTLD)
}
