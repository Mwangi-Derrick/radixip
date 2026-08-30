use ipnetwork::IpNetwork;
use radixip::{EngineVariant, Metadata, NodeVariant, RadixEngine, engine::EngineWrapper};
use std::net::IpAddr;

#[test]
fn test_all_rust_engine_and_node_combinations() {
    let engine_variants = vec![
        EngineVariant::Standard,
        EngineVariant::Concurrent,
        EngineVariant::LockFree,
        EngineVariant::Adaptive,
    ];

    let node_variants = vec![
        NodeVariant::NormalTrieNode,
        NodeVariant::AtomicTrieNode,
        NodeVariant::PaddedTrieNode,
        NodeVariant::LockFreeTrieNode,
        NodeVariant::NormalRadixNode,
        NodeVariant::AtomicRadixNode,
        NodeVariant::PaddedRadixNode,
        NodeVariant::LockFreeRadixNode,
    ];

    for ev in &engine_variants {
        for nv in &node_variants {
            for compressed in &[false, true] {
                let engine = EngineWrapper::new(*ev, *nv, *compressed, 32);

                assert_eq!(engine.size(), 0);

                // Insert broad
                let broad_prefix = "10.0.0.0/8".parse::<IpNetwork>().unwrap();
                engine.insert(broad_prefix, Metadata::new("broad")).unwrap();

                // Insert specific
                let specific_prefix = "10.1.2.0/24".parse::<IpNetwork>().unwrap();
                engine
                    .insert(specific_prefix, Metadata::new("specific"))
                    .unwrap();

                // Check contains
                assert!(engine.contains(&broad_prefix));

                // LPM lookup
                let ip_specific = "10.1.2.99".parse::<IpAddr>().unwrap();
                assert_eq!(engine.lookup(&ip_specific), Some(Metadata::new("specific")));

                // Fallback lookup
                let ip_broad = "10.2.0.1".parse::<IpAddr>().unwrap();
                assert_eq!(engine.lookup(&ip_broad), Some(Metadata::new("broad")));

                // Lookup miss
                let ip_miss = "192.168.1.1".parse::<IpAddr>().unwrap();
                assert_eq!(engine.lookup(&ip_miss), None);

                // Remove specific
                let removed = engine.remove(&specific_prefix);
                assert_eq!(removed, Some(Metadata::new("specific")));

                // Should fallback to broad after removal
                assert_eq!(engine.lookup(&ip_specific), Some(Metadata::new("broad")));

                // Clear
                engine.clear();
                assert_eq!(engine.size(), 0);
            }
        }
    }
}

#[test]
fn test_ipv6_longest_prefix_match() {
    for compressed in &[false, true] {
        let engine = EngineWrapper::new(
            EngineVariant::Standard,
            NodeVariant::AtomicRadixNode,
            *compressed,
            64 as usize,
        );

        let broad_v6 = "2001:db8::/32".parse::<IpNetwork>().unwrap();
        let specific_v6 = "2001:db8:85a3::/48".parse::<IpNetwork>().unwrap();

        engine.insert(broad_v6, Metadata::new("v6-broad")).unwrap();
        engine
            .insert(specific_v6, Metadata::new("v6-specific"))
            .unwrap();

        let ip_specific = "2001:db8:85a3:0000:0000:8a2e:0370:7334"
            .parse::<IpAddr>()
            .unwrap();
        assert_eq!(
            engine.lookup(&ip_specific),
            Some(Metadata::new("v6-specific"))
        );

        let ip_broad = "2001:db8:9999::1".parse::<IpAddr>().unwrap();
        assert_eq!(engine.lookup(&ip_broad), Some(Metadata::new("v6-broad")));
    }
}

#[test]
fn cached_engine_invalidates_ips_under_changed_prefix() {
    let engine = EngineWrapper::new(EngineVariant::Standard, NodeVariant::Padded, false);

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
