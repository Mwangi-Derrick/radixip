use std::net::IpAddr;

use ipnetwork::IpNetwork;
use radixip::{
    CacheConfig, CachedEngine, Metadata, NodeVariant, RadixEngine, StandardEngine,
};

#[test]
fn lookup_returns_longest_prefix_match() {
    let engine = StandardEngine::new(NodeVariant::Normal);

    engine
        .insert(
            "10.0.0.0/8".parse::<IpNetwork>().unwrap(),
            Metadata::new("broad"),
        )
        .unwrap();
    engine
        .insert(
            "10.1.2.0/24".parse::<IpNetwork>().unwrap(),
            Metadata::new("specific"),
        )
        .unwrap();

    let ip = "10.1.2.99".parse::<IpAddr>().unwrap();
    assert_eq!(engine.lookup(&ip), Some(Metadata::new("specific")));
}

#[test]
fn cached_engine_invalidates_ips_under_changed_prefix() {
    let inner = std::sync::Arc::new(StandardEngine::new(NodeVariant::Atomic));
    let engine = CachedEngine::new(
        inner,
        CacheConfig {
            max_entries: 32,
            ttl_seconds: None,
        },
    );

    engine
        .insert(
            "192.168.0.0/16".parse::<IpNetwork>().unwrap(),
            Metadata::new("allow"),
        )
        .unwrap();

    let ip = "192.168.1.10".parse::<IpAddr>().unwrap();
    assert_eq!(engine.lookup(&ip), Some(Metadata::new("allow")));

    engine
        .insert(
            "192.168.1.0/24".parse::<IpNetwork>().unwrap(),
            Metadata::new("deny"),
        )
        .unwrap();

    assert_eq!(engine.lookup(&ip), Some(Metadata::new("deny")));
}
