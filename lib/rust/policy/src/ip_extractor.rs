//! IP Extraction from HTTP headers.
//!
//! Supports X-Forwarded-For (with trusted-proxy stripping),
//! X-Real-IP, and the raw socket remote address.

use ipnetwork::IpNetwork;
use std::net::{IpAddr, SocketAddr};
use thiserror::Error;

#[derive(Debug, Error)]
pub enum ExtractError {
    #[error("No usable IP found in headers")]
    NotFound,
    #[error("Invalid IP address: {0}")]
    Invalid(String),
}

/// Extract the real client IP from a raw header map.
///
/// `xff` — value of the `X-Forwarded-For` header, if present.
/// `x_real_ip` — value of the `X-Real-IP` header, if present.
/// `remote_addr` — the raw socket peer address.
/// `trusted_proxies` — CIDRs whose IPs are stripped from the XFF chain.
pub fn extract_ip(
    xff: Option<&str>,
    x_real_ip: Option<&str>,
    remote_addr: Option<&SocketAddr>,
    trusted_proxies: &[IpNetwork],
) -> Result<IpAddr, ExtractError> {
    // 1. Try X-Forwarded-For — walk right-to-left stripping trusted hops.
    if let Some(xff_val) = xff {
        let ips: Vec<&str> = xff_val.split(',').map(str::trim).collect();
        // Walk from right (closest proxy) toward left (original client).
        for raw in ips.iter().rev() {
            let ip: IpAddr = raw.parse().map_err(|_| ExtractError::Invalid(raw.to_string()))?;
            if !is_trusted(ip, trusted_proxies) {
                return Ok(ip);
            }
        }
    }

    // 2. Fallback: X-Real-IP.
    if let Some(rip) = x_real_ip {
        return rip.parse().map_err(|_| ExtractError::Invalid(rip.to_string()));
    }

    // 3. Fallback: raw socket addr.
    if let Some(addr) = remote_addr {
        return Ok(addr.ip());
    }

    Err(ExtractError::NotFound)
}

#[inline]
fn is_trusted(ip: IpAddr, trusted: &[IpNetwork]) -> bool {
    trusted.iter().any(|net| net.contains(ip))
}

#[cfg(test)]
mod tests {
    use super::*;

    fn trusted() -> Vec<IpNetwork> {
        vec!["127.0.0.1/32".parse().unwrap(), "10.0.0.0/8".parse().unwrap()]
    }

    #[test]
    fn xff_strips_trusted_proxies() {
        // XFF chain: client → corp proxy (10.x) → localhost
        // Expected: first non-trusted from the right = 203.0.113.5
        let ip = extract_ip(
            Some("203.0.113.5, 10.1.2.3, 127.0.0.1"),
            None,
            None,
            &trusted(),
        )
        .unwrap();
        assert_eq!(ip, "203.0.113.5".parse::<IpAddr>().unwrap());
    }

    #[test]
    fn falls_back_to_x_real_ip() {
        let ip = extract_ip(None, Some("198.51.100.7"), None, &trusted()).unwrap();
        assert_eq!(ip, "198.51.100.7".parse::<IpAddr>().unwrap());
    }

    #[test]
    fn falls_back_to_remote_addr() {
        use std::net::SocketAddr;
        let addr: SocketAddr = "192.0.2.1:12345".parse().unwrap();
        let ip = extract_ip(None, None, Some(&addr), &trusted()).unwrap();
        assert_eq!(ip, "192.0.2.1".parse::<IpAddr>().unwrap());
    }
}
