package core

// AuthResult holds the routing information extracted from a successful
// Proxy-Authorization header.
type AuthResult struct {
	// Sub is the subscriber identity parsed from the "sub" field of the username JSON.
	Sub string
	// SetName is the proxy set to route this request through.
	SetName string
	// AffinityMinutes is the sticky session duration (0 = no affinity).
	AffinityMinutes uint16
	// UsernameB64 is the raw base64 username string used as the affinity map key.
	UsernameB64 string
	// AffinityParams are the decoded metadata fields from the username.
	AffinityParams AffinityParams
}

// AuthProvider validates proxy credentials.
// Authenticate receives the parsed sub and raw password and returns nil to
// allow the request, or an error to reject it.
type AuthProvider interface {
	Authenticate(sub, password string) error
}

// ConnectionTracker is an optional interface AuthProviders can implement to
// observe and control individual proxied connections.
//
// The gateway checks for this interface via type assertion after a successful
// Authenticate call. When implemented:
//
//  1. OpenConnection is called before the upstream tunnel is established.
//     Returning an error aborts the connection immediately (e.g. concurrent
//     connection limit exceeded). On success it returns a ConnHandle that is
//     used for all subsequent callbacks on this connection.
//
//  2. The gateway calls ConnHandle.RecordTraffic periodically during the
//     relay with incremental byte counts. The implementation may call the
//     provided cancel() function at any time to tear down the connection
//     mid-transfer (e.g. bandwidth cap reached).
//
//  3. ConnHandle.Close is called exactly once when the connection ends,
//     with the final total bytes for the full connection lifetime.
//
// Implementations must be safe for concurrent use across many goroutines.
type ConnectionTracker interface {
	// OpenConnection is called before the upstream tunnel opens.
	// sub is the authenticated subscriber identity.
	// Returns a ConnHandle for this connection, or an error to abort.
	OpenConnection(sub string) (ConnHandle, error)
}

// ConnHandle tracks a single active connection.
// All methods must be safe for concurrent use.
type ConnHandle interface {
	// RecordTraffic is called with incremental byte counts as data flows.
	// upstream is true when bytes flowed client→upstream, false for upstream→client.
	// cancel is a function that tears down the connection immediately when called;
	// the implementation should call it when a limit is exceeded mid-transfer.
	RecordTraffic(upstream bool, delta int64, cancel func())

	// Close is called exactly once when the connection ends.
	// sentTotal and receivedTotal are the full byte counts for the connection.
	Close(sentTotal, receivedTotal int64)
}
