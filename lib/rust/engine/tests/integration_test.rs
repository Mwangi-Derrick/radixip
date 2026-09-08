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
                let engine = EngineWrapper::new(*ev, *nv, *compressed, Some(32));

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
            Some(64),
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
    let engine = EngineWrapper::new(
        EngineVariant::Standard,
        NodeVariant::PaddedRadixNode,
        false,
        Some(32),
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

#[tokio::test]
async fn redis_sync_smoke_test() {
    let client = radixip::redis::RedisClient::new(radixip::redis::RedisConfig {
        url: "redis://127.0.0.1:6379".to_string(),
        pool_size: 4,
        connect_timeout: std::time::Duration::from_secs(5),
        max_retries: 2,
    })
    .await
    .expect("redis must be running via docker compose up -d redis");

    let key = format!("radixip:test:lookup:{}", std::process::id());
    let hash_key = format!("radixip:test:entries:{}", std::process::id());
    let channel = format!("radixip:test:updates:{}", std::process::id());

    client
        .set_sync(&key, "{\"value\":\"allow\"}")
        .expect("set_sync should succeed");
    assert_eq!(
        client.get_sync(&key).expect("get_sync should succeed").as_deref(),
        Some("{\"value\":\"allow\"}")
    );

    let prefix: IpNetwork = "10.0.0.0/8".parse().unwrap();
    let update = radixip::redis::RedisCacheUpdate::Insert {
        prefix,
        metadata: serde_json::json!({
            "value": "allow",
            "attributes": { "region": "test" }
        }),
    };

    client
        .publish_json(&channel, &update)
        .await
        .expect("publish_json should succeed");

    let (mut rx, _handle) = client
        .subscribe_to_channel(&channel)
        .await
        .expect("subscribe_to_channel should succeed");

    let payload = tokio::time::timeout(std::time::Duration::from_secs(5), async {
        while let Some(msg) = rx.recv().await {
            if msg.channel == channel {
                return msg.payload;
            }
        }
        panic!("channel unexpectedly closed")
    })
    .await
    .expect("timed out waiting for Redis pub/sub message");

    let decoded: radixip::redis::RedisCacheUpdate = serde_json::from_str(&payload).unwrap();
    match decoded {
        radixip::redis::RedisCacheUpdate::Insert { prefix: sent_prefix, .. } => {
            assert_eq!(sent_prefix, prefix);
        }
        other => panic!("expected insert update, got {other:?}"),
    }

    client
        .hset_sync(&hash_key, "10.0.0.0/8", "{\"value\":\"allow\"}")
        .expect("hset_sync should succeed");

    let entries = client
        .hgetall_sync(&hash_key)
        .expect("hgetall_sync should succeed");
    assert!(entries.contains_key("10.0.0.0/8"));

    client
        .hdel_sync(&hash_key, "10.0.0.0/8")
        .expect("hdel_sync should succeed");
    assert!(!client
        .hgetall_sync(&hash_key)
        .expect("hgetall_sync should succeed")
        .contains_key("10.0.0.0/8"));
}
