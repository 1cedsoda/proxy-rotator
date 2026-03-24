use anyhow::{Context, Result};
use proxy_gateway_core::{parse_proxy_line, ProxyFormat, SourceProxy};
use std::path::Path;

/// Parse a proxies file using the given format.
///
/// Each non-empty, non-comment line is parsed as a proxy entry.
/// Comments with `#` and blank lines are skipped.
pub fn load_proxies(path: &Path, format: ProxyFormat) -> Result<Vec<SourceProxy>> {
    let content =
        std::fs::read_to_string(path).with_context(|| format!("reading {}", path.display()))?;
    let mut proxies = Vec::new();
    for (i, line) in content.lines().enumerate() {
        let line = line.trim();
        if line.is_empty() || line.starts_with('#') {
            continue;
        }
        let proxy = parse_proxy_line(line, format).with_context(|| {
            format!(
                "{}:{}: invalid proxy entry '{}'",
                path.display(),
                i + 1,
                line
            )
        })?;
        proxies.push(proxy);
    }
    Ok(proxies)
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::Write;
    use tempfile::NamedTempFile;

    #[test]
    fn test_load_host_port_user_pass_format() {
        let mut f = NamedTempFile::new().unwrap();
        writeln!(f, "# comment").unwrap();
        writeln!(f, "198.51.100.1:6658:myuser:mypass").unwrap();
        writeln!(f, "").unwrap();
        writeln!(f, "198.51.100.2:7872:myuser:mypass").unwrap();
        writeln!(f, "plain.proxy.com:3128").unwrap();
        f.flush().unwrap();

        let proxies = load_proxies(f.path(), ProxyFormat::HostPortUserPass).unwrap();
        assert_eq!(proxies.len(), 3);
        assert_eq!(proxies[0].host, "198.51.100.1");
        assert_eq!(proxies[0].port, 6658);
        assert_eq!(proxies[0].username.as_deref(), Some("myuser"));
        assert_eq!(proxies[2].host, "plain.proxy.com");
        assert!(proxies[2].username.is_none());
    }

    #[test]
    fn test_load_user_pass_at_host_port_format() {
        let mut f = NamedTempFile::new().unwrap();
        writeln!(f, "myuser:mypass@198.51.100.1:6658").unwrap();
        writeln!(f, "plain.proxy.com:3128").unwrap();
        f.flush().unwrap();

        let proxies = load_proxies(f.path(), ProxyFormat::UserPassAtHostPort).unwrap();
        assert_eq!(proxies.len(), 2);
        assert_eq!(proxies[0].host, "198.51.100.1");
        assert_eq!(proxies[0].username.as_deref(), Some("myuser"));
        assert!(proxies[1].username.is_none());
    }

    #[test]
    fn test_load_user_pass_host_port_format() {
        let mut f = NamedTempFile::new().unwrap();
        writeln!(f, "myuser:mypass:198.51.100.1:6658").unwrap();
        f.flush().unwrap();

        let proxies = load_proxies(f.path(), ProxyFormat::UserPassHostPort).unwrap();
        assert_eq!(proxies.len(), 1);
        assert_eq!(proxies[0].host, "198.51.100.1");
        assert_eq!(proxies[0].username.as_deref(), Some("myuser"));
    }
}
