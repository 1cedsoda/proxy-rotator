package core

import "context"

// ProxySource is the abstraction over "give me the next upstream proxy endpoint".
// Implementations must be safe for concurrent use.
type ProxySource interface {
	// GetSourceProxy returns the next upstream proxy to use.
	// Returns nil if no proxy is currently available.
	GetSourceProxy(ctx context.Context, params AffinityParams) (*SourceProxy, error)

	// GetSourceProxyForceRotate returns a different proxy for force-rotation.
	// current is the proxy the session is currently pinned to.
	// The default behaviour is to call GetSourceProxy again.
	GetSourceProxyForceRotate(ctx context.Context, params AffinityParams, current *SourceProxy) (*SourceProxy, error)

	// Describe returns a human-readable description for log messages.
	Describe() string
}
