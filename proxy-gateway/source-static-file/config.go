package staticfile

import "proxy-gateway/core"

// Config is the configuration for a static-file proxy source.
type Config struct {
	// Path to the proxies file (one proxy per line).
	// Relative paths are resolved against the config file directory.
	ProxiesFile string `toml:"proxies_file" yaml:"proxies_file" json:"proxies_file"`

	// Format of each proxy line. Defaults to host_port_user_pass.
	Format core.ProxyFormat `toml:"format" yaml:"format" json:"format"`
}
