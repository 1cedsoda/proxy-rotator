package core

// SourceProxy is a parsed upstream proxy entry with optional credentials.
type SourceProxy struct {
	Host     string
	Port     uint16
	Username *string
	Password *string
}

func (s SourceProxy) Equal(other SourceProxy) bool {
	if s.Host != other.Host || s.Port != other.Port {
		return false
	}
	usernameEq := (s.Username == nil) == (other.Username == nil) &&
		(s.Username == nil || *s.Username == *other.Username)
	passwordEq := (s.Password == nil) == (other.Password == nil) &&
		(s.Password == nil || *s.Password == *other.Password)
	return usernameEq && passwordEq
}
