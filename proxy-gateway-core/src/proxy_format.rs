//! Proxy line format parsing.
//!
//! Supports three common proxy line formats, all with an optional
//! `protocol://` prefix (which is stripped and ignored):
//!
//! - [`ProxyFormat::UserPassAtHostPort`] — `user:pass@host:port`
//! - [`ProxyFormat::UserPassHostPort`] — `user:pass:host:port`
//! - [`ProxyFormat::HostPortUserPass`] — `host:port:user:pass`
//!
//! Lines without credentials (`host:port`) are accepted by all formats.
//! IPv6 addresses in brackets (`[::1]:port`) are supported.

use crate::SourceProxy;
use anyhow::{Context, Result};

/// Supported proxy line formats.
#[derive(Debug, Clone, Copy, PartialEq, Eq, serde::Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum ProxyFormat {
    /// `username:password@host:port` (with optional `protocol://` prefix)
    UserPassAtHostPort,
    /// `username:password:host:port`
    UserPassHostPort,
    /// `host:port:username:password` (legacy default)
    HostPortUserPass,
}

impl Default for ProxyFormat {
    fn default() -> Self {
        Self::HostPortUserPass
    }
}

/// Parse a single proxy line in the given format.
///
/// An optional `protocol://` prefix (e.g. `http://`, `socks5://`) is stripped.
/// Lines without credentials (`host:port`) are accepted by all formats.
pub fn parse_proxy_line(s: &str, format: ProxyFormat) -> Result<SourceProxy> {
    // Strip optional protocol prefix.
    let s = strip_protocol(s);

    match format {
        ProxyFormat::UserPassAtHostPort => parse_user_pass_at_host_port(s),
        ProxyFormat::UserPassHostPort => parse_user_pass_host_port(s),
        ProxyFormat::HostPortUserPass => parse_host_port_user_pass(s),
    }
}

/// Strip an optional `protocol://` prefix.
fn strip_protocol(s: &str) -> &str {
    match s.find("://") {
        Some(i) => &s[i + 3..],
        None => s,
    }
}

// ---------------------------------------------------------------------------
// Format: user:pass@host:port
// ---------------------------------------------------------------------------

fn parse_user_pass_at_host_port(s: &str) -> Result<SourceProxy> {
    match s.rfind('@') {
        Some(at) => {
            let creds = &s[..at];
            let host_port = &s[at + 1..];
            let (host, port) = parse_host_port(host_port)?;
            let (user, pass) = split_user_pass(creds)?;
            Ok(SourceProxy {
                host,
                port,
                username: Some(user),
                password: Some(pass),
            })
        }
        None => {
            // No @ — treat as host:port without credentials.
            let (host, port) = parse_host_port(s)?;
            Ok(SourceProxy {
                host,
                port,
                username: None,
                password: None,
            })
        }
    }
}

// ---------------------------------------------------------------------------
// Format: user:pass:host:port
// ---------------------------------------------------------------------------

fn parse_user_pass_host_port(s: &str) -> Result<SourceProxy> {
    // Try 4-part split first (user:pass:host:port).
    // Ambiguity: we split from the right to find host:port, since the
    // password could contain colons in theory, but port is always numeric.
    let parts: Vec<&str> = s.rsplitn(3, ':').collect();
    // rsplitn(3, ':') on "u:p:h:port" gives ["port", "h", "u:p"]
    if parts.len() == 3 {
        if let Ok(port) = parts[0].parse::<u16>() {
            let host = parts[1];
            let creds = parts[2];
            if let Some(colon) = creds.find(':') {
                let user = &creds[..colon];
                let pass = &creds[colon + 1..];
                if !user.is_empty() {
                    return Ok(SourceProxy {
                        host: host.to_string(),
                        port,
                        username: Some(user.to_string()),
                        password: Some(pass.to_string()),
                    });
                }
            }
        }
    }

    // Fall back to host:port (no credentials).
    if parts.len() >= 2 {
        if let Ok(port) = parts[0].parse::<u16>() {
            // For 2-part: rsplitn gives ["port", "host"]
            // For 3-part where above failed: try treating it as host:port
            let host = if parts.len() == 2 {
                parts[1]
            } else {
                // Rejoin everything before the last colon
                let last_colon = s.rfind(':').unwrap();
                &s[..last_colon]
            };
            // Only accept as host:port if it looks like a host (no extra colons unless ipv6)
            if !host.contains(':') || host.starts_with('[') {
                return Ok(SourceProxy {
                    host: host.to_string(),
                    port,
                    username: None,
                    password: None,
                });
            }
        }
    }

    anyhow::bail!("expected user:pass:host:port or host:port, got '{s}'")
}

// ---------------------------------------------------------------------------
// Format: host:port:user:pass
// ---------------------------------------------------------------------------

fn parse_host_port_user_pass(s: &str) -> Result<SourceProxy> {
    // Handle IPv6 in brackets.
    if s.starts_with('[') {
        let bracket_end = s
            .find(']')
            .ok_or_else(|| anyhow::anyhow!("unclosed bracket in '{s}'"))?;
        let host = s[1..bracket_end].to_string();
        let rest = s[bracket_end + 1..]
            .strip_prefix(':')
            .ok_or_else(|| anyhow::anyhow!("expected ':' after ']' in '{s}'"))?;
        return parse_port_and_optional_creds(&host, rest);
    }

    let parts: Vec<&str> = s.splitn(4, ':').collect();
    match parts.len() {
        2 => {
            let port: u16 = parts[1].parse().context("invalid port")?;
            Ok(SourceProxy {
                host: parts[0].to_string(),
                port,
                username: None,
                password: None,
            })
        }
        4 => {
            let port: u16 = parts[1].parse().context("invalid port")?;
            Ok(SourceProxy {
                host: parts[0].to_string(),
                port,
                username: Some(parts[2].to_string()),
                password: Some(parts[3].to_string()),
            })
        }
        _ => anyhow::bail!("expected host:port or host:port:user:pass, got '{s}'"),
    }
}

fn parse_port_and_optional_creds(host: &str, rest: &str) -> Result<SourceProxy> {
    let parts: Vec<&str> = rest.splitn(3, ':').collect();
    match parts.len() {
        1 => {
            let port: u16 = parts[0].parse().context("invalid port")?;
            Ok(SourceProxy {
                host: host.to_string(),
                port,
                username: None,
                password: None,
            })
        }
        3 => {
            let port: u16 = parts[0].parse().context("invalid port")?;
            Ok(SourceProxy {
                host: host.to_string(),
                port,
                username: Some(parts[1].to_string()),
                password: Some(parts[2].to_string()),
            })
        }
        _ => anyhow::bail!("expected port or port:user:pass after host, got '{rest}'"),
    }
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

/// Parse `host:port` or `[ipv6]:port`.
fn parse_host_port(s: &str) -> Result<(String, u16)> {
    if s.starts_with('[') {
        let bracket_end = s
            .find(']')
            .ok_or_else(|| anyhow::anyhow!("unclosed bracket in '{s}'"))?;
        let host = s[1..bracket_end].to_string();
        let rest = s[bracket_end + 1..]
            .strip_prefix(':')
            .ok_or_else(|| anyhow::anyhow!("expected ':' after ']' in '{s}'"))?;
        let port: u16 = rest.parse().context("invalid port")?;
        Ok((host, port))
    } else {
        let colon = s
            .rfind(':')
            .ok_or_else(|| anyhow::anyhow!("expected host:port, got '{s}'"))?;
        let host = &s[..colon];
        let port: u16 = s[colon + 1..].parse().context("invalid port")?;
        Ok((host.to_string(), port))
    }
}

/// Split `user:pass` at the first colon.
fn split_user_pass(s: &str) -> Result<(String, String)> {
    let colon = s
        .find(':')
        .ok_or_else(|| anyhow::anyhow!("expected user:pass, got '{s}'"))?;
    Ok((s[..colon].to_string(), s[colon + 1..].to_string()))
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    use super::*;

    // -- UserPassAtHostPort --

    #[test]
    fn test_upahp_basic() {
        let p = parse_proxy_line("user:pass@host.com:8080", ProxyFormat::UserPassAtHostPort).unwrap();
        assert_eq!(p.host, "host.com");
        assert_eq!(p.port, 8080);
        assert_eq!(p.username.as_deref(), Some("user"));
        assert_eq!(p.password.as_deref(), Some("pass"));
    }

    #[test]
    fn test_upahp_with_protocol() {
        let p = parse_proxy_line("http://user:pass@host.com:3128", ProxyFormat::UserPassAtHostPort).unwrap();
        assert_eq!(p.host, "host.com");
        assert_eq!(p.port, 3128);
        assert_eq!(p.username.as_deref(), Some("user"));
    }

    #[test]
    fn test_upahp_socks5() {
        let p = parse_proxy_line("socks5://user:pass@host.com:1080", ProxyFormat::UserPassAtHostPort).unwrap();
        assert_eq!(p.host, "host.com");
        assert_eq!(p.port, 1080);
    }

    #[test]
    fn test_upahp_no_creds() {
        let p = parse_proxy_line("host.com:8080", ProxyFormat::UserPassAtHostPort).unwrap();
        assert_eq!(p.host, "host.com");
        assert_eq!(p.port, 8080);
        assert!(p.username.is_none());
    }

    #[test]
    fn test_upahp_complex_username() {
        // Bottingtools-style username with underscores and hyphens
        let p = parse_proxy_line(
            "exampleuser_pool-custom_type-high:XpmTeTdYy8hT@proxy.bottingtools.com:1337",
            ProxyFormat::UserPassAtHostPort,
        ).unwrap();
        assert_eq!(p.host, "proxy.bottingtools.com");
        assert_eq!(p.port, 1337);
        assert_eq!(p.username.as_deref(), Some("exampleuser_pool-custom_type-high"));
        assert_eq!(p.password.as_deref(), Some("XpmTeTdYy8hT"));
    }

    #[test]
    fn test_upahp_ipv6() {
        let p = parse_proxy_line("user:pass@[::1]:3128", ProxyFormat::UserPassAtHostPort).unwrap();
        assert_eq!(p.host, "::1");
        assert_eq!(p.port, 3128);
        assert_eq!(p.username.as_deref(), Some("user"));
    }

    // -- UserPassHostPort --

    #[test]
    fn test_uphp_basic() {
        let p = parse_proxy_line("user:pass:host.com:8080", ProxyFormat::UserPassHostPort).unwrap();
        assert_eq!(p.host, "host.com");
        assert_eq!(p.port, 8080);
        assert_eq!(p.username.as_deref(), Some("user"));
        assert_eq!(p.password.as_deref(), Some("pass"));
    }

    #[test]
    fn test_uphp_no_creds() {
        let p = parse_proxy_line("host.com:8080", ProxyFormat::UserPassHostPort).unwrap();
        assert_eq!(p.host, "host.com");
        assert_eq!(p.port, 8080);
        assert!(p.username.is_none());
    }

    #[test]
    fn test_uphp_with_protocol() {
        let p = parse_proxy_line("http://user:pass:host.com:3128", ProxyFormat::UserPassHostPort).unwrap();
        assert_eq!(p.host, "host.com");
        assert_eq!(p.port, 3128);
        assert_eq!(p.username.as_deref(), Some("user"));
    }

    // -- HostPortUserPass --

    #[test]
    fn test_hpup_basic() {
        let p = parse_proxy_line("host.com:8080:user:pass", ProxyFormat::HostPortUserPass).unwrap();
        assert_eq!(p.host, "host.com");
        assert_eq!(p.port, 8080);
        assert_eq!(p.username.as_deref(), Some("user"));
        assert_eq!(p.password.as_deref(), Some("pass"));
    }

    #[test]
    fn test_hpup_no_creds() {
        let p = parse_proxy_line("host.com:8080", ProxyFormat::HostPortUserPass).unwrap();
        assert_eq!(p.host, "host.com");
        assert_eq!(p.port, 8080);
        assert!(p.username.is_none());
    }

    #[test]
    fn test_hpup_ipv6() {
        let p = parse_proxy_line("[::1]:3128", ProxyFormat::HostPortUserPass).unwrap();
        assert_eq!(p.host, "::1");
        assert_eq!(p.port, 3128);
    }

    #[test]
    fn test_hpup_ipv6_with_creds() {
        let p = parse_proxy_line("[2001:db8::1]:8080:user:pass", ProxyFormat::HostPortUserPass).unwrap();
        assert_eq!(p.host, "2001:db8::1");
        assert_eq!(p.port, 8080);
        assert_eq!(p.username.as_deref(), Some("user"));
    }

    #[test]
    fn test_hpup_with_protocol() {
        let p = parse_proxy_line("http://host.com:8080:user:pass", ProxyFormat::HostPortUserPass).unwrap();
        assert_eq!(p.host, "host.com");
        assert_eq!(p.port, 8080);
        assert_eq!(p.username.as_deref(), Some("user"));
    }

    #[test]
    fn test_hpup_bad_format() {
        assert!(parse_proxy_line("host:port:only_three", ProxyFormat::HostPortUserPass).is_err());
        assert!(parse_proxy_line("justhost", ProxyFormat::HostPortUserPass).is_err());
    }
}
