use proxy_gateway_core::ProxyFormat;

/// Configuration for a static-file proxy source.
#[derive(Debug, Clone, serde::Deserialize)]
pub struct StaticFileConfig {
    /// Path to the proxies file (one proxy per line).
    ///
    /// Relative paths are resolved against the directory that contains the
    /// main `config.toml` file.
    pub proxies_file: std::path::PathBuf,

    /// Line format for the proxies file.
    ///
    /// - `host_port_user_pass` — `host:port:user:pass` (default)
    /// - `user_pass_at_host_port` — `user:pass@host:port`
    /// - `user_pass_host_port` — `user:pass:host:port`
    ///
    /// All formats accept `host:port` (no auth) and optional `protocol://` prefixes.
    #[serde(default)]
    pub format: ProxyFormat,
}
