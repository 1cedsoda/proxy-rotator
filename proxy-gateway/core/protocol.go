package core

// Protocol is the proxy protocol used to connect to the upstream.
type Protocol string

const (
	ProtocolHTTP   Protocol = "http"
	ProtocolSOCKS5 Protocol = "socks5"
)
