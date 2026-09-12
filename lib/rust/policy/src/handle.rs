//! Compact policy handle for native and language bindings.

use crate::{PolicyDecision, PolicyEngine, RouteTrie};
use radixip::{new_balanced, RadixEngine};
use radixip_config::RadixIpConfig;
use std::net::{IpAddr, Ipv4Addr, Ipv6Addr};
use std::sync::Arc;

/// Fixed-size IP representation suitable for a C ABI or native binding.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct PackedIp {
    /// Address family: 4 for IPv4, 6 for IPv6.
    pub family: u8,
    /// IPv4 uses the first four bytes; IPv6 uses all sixteen bytes.
    pub bytes: [u8; 16],
}

impl PackedIp {
    pub fn into_ip_addr(self) -> Option<IpAddr> {
        match self.family {
            4 => Some(IpAddr::V4(Ipv4Addr::new(
                self.bytes[0],
                self.bytes[1],
                self.bytes[2],
                self.bytes[3],
            ))),
            6 => Some(IpAddr::V6(Ipv6Addr::from(self.bytes))),
            _ => None,
        }
    }
}

impl From<IpAddr> for PackedIp {
    fn from(ip: IpAddr) -> Self {
        match ip {
            IpAddr::V4(ip) => {
                let mut bytes = [0; 16];
                bytes[..4].copy_from_slice(&ip.octets());
                Self { family: 4, bytes }
            }
            IpAddr::V6(ip) => Self {
                family: 6,
                bytes: ip.octets(),
            },
        }
    }
}

/// Stable, binding-friendly result for one policy decision.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct PolicyResult {
    pub decision: PolicyDecisionCode,
    pub retry_after_seconds: u32,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
#[repr(u8)]
pub enum PolicyDecisionCode {
    Allow = 0,
    Block = 1,
    Limit = 2,
    BadRequest = 3,
}

impl PolicyResult {
    fn from_decision(decision: PolicyDecision) -> Self {
        let code = match decision {
            PolicyDecision::Allow => PolicyDecisionCode::Allow,
            PolicyDecision::Block | PolicyDecision::AutoBanned => PolicyDecisionCode::Block,
            PolicyDecision::Limit => PolicyDecisionCode::Limit,
            PolicyDecision::BadRequest(_) => PolicyDecisionCode::BadRequest,
        };
        let retry_after_seconds = u32::from(code == PolicyDecisionCode::Limit);
        Self {
            decision: code,
            retry_after_seconds,
        }
    }
}

/// Owns one shared engine and one policy implementation.
pub struct PolicyHandle {
    policy: PolicyEngine,
    route_trie: Option<RouteTrie>,
}

impl PolicyHandle {
    pub fn new(engine: Arc<Box<dyn RadixEngine>>, config: &RadixIpConfig) -> Self {
        let mut policy = PolicyEngine::new(
            engine.clone(),
            config.radixip.middleware.clone(),
            config.radixip.rate_limit.clone(),
            config.radixip.blocklist.enabled,
        );
        if config.radixip.auto_ban.enabled {
            policy =
                policy.with_auto_ban(crate::AutoBanTracker::new(&config.radixip.auto_ban, engine));
        }
        let route_trie = if config.radixip.rate_limit_routes.enabled {
            let mut trie = RouteTrie::new();
            for route in &config.radixip.rate_limit_routes.routes {
                let methods: Vec<&str> = route.methods.iter().map(String::as_str).collect();
                trie.insert(&route.path, &methods, route.rate_limit.clone());
            }
            Some(trie)
        } else {
            None
        };
        Self { policy, route_trie }
    }

    pub fn check(&self, ip: PackedIp) -> Option<PolicyResult> {
        ip.into_ip_addr()
            .map(|ip| PolicyResult::from_decision(self.policy.check_ip(ip)))
    }

    /// Evaluate a request with the route-specific rate limit, if configured.
    /// `method` and `path` are supplied by the host framework after it has
    /// extracted the client IP from its trusted transport context.
    pub fn check_request(&self, ip: PackedIp, method: &str, path: &str) -> Option<PolicyResult> {
        ip.into_ip_addr().map(|ip| {
            let route_limiter = self
                .route_trie
                .as_ref()
                .and_then(|trie| trie.match_route(method, path));
            PolicyResult::from_decision(self.policy.check_ip_with_limiter(ip, route_limiter))
        })
    }

    pub fn policy(&self) -> &PolicyEngine {
        &self.policy
    }
}

/// Convenience constructor for bindings that want the balanced default engine.
pub async fn from_config(config: RadixIpConfig) -> PolicyHandle {
    let engine = Arc::new(new_balanced().await);
    PolicyHandle::new(engine, &config)
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::net::IpAddr;

    #[test]
    fn packed_ipv4_round_trips() {
        let original: IpAddr = "192.0.2.42".parse().unwrap();
        let packed = PackedIp::from(original);

        assert_eq!(packed.family, 4);
        assert_eq!(packed.into_ip_addr(), Some(original));
    }

    #[test]
    fn packed_ipv6_round_trips() {
        let original: IpAddr = "2001:db8::42".parse().unwrap();
        let packed = PackedIp::from(original);

        assert_eq!(packed.family, 6);
        assert_eq!(packed.into_ip_addr(), Some(original));
    }

    #[test]
    fn invalid_family_is_rejected() {
        assert_eq!(
            PackedIp {
                family: 5,
                bytes: [0; 16]
            }
            .into_ip_addr(),
            None
        );
    }

    #[test]
    fn result_codes_are_stable() {
        assert_eq!(PolicyDecisionCode::Allow as u8, 0);
        assert_eq!(PolicyDecisionCode::Block as u8, 1);
        assert_eq!(PolicyDecisionCode::Limit as u8, 2);
        assert_eq!(PolicyDecisionCode::BadRequest as u8, 3);
    }
}
