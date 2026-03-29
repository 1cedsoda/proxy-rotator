package core

// Proxy is a resolved upstream proxy endpoint.
type Proxy struct {
	Host     string
	Port     uint16
	Username string
	Password string
	Protocol Protocol
}

// Proto returns the protocol, defaulting to HTTP if empty.
func (p *Proxy) Proto() Protocol {
	if p.Protocol == "" {
		return ProtocolHTTP
	}
	return p.Protocol
}
