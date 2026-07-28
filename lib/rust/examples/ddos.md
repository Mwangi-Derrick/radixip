```rust
use ipnetwork::IpNetwork;
use std::collections::HashMap;
use std::net::IpAddr;

// Explicitly using your library architecture
use radixip::tree::UncompressedTree;
use radixip::{Metadata, NodeVariant, RadixEngine, StandardEngine};

fn main() {
    // 1. Initialize your Radix Engine using the Atomic variant
    let engine = StandardEngine::new(UncompressedTree::new(NodeVariant::Atomic));

    // 2. Setup attributes for a massive DDoS attack originating from a known botnet subnet
    let mut ddos_attrs = HashMap::new();
    ddos_attrs.insert("reason".to_string(), "Volumetric Layer 7 DDoS Attack".to_string());
    ddos_attrs.insert("mitigation_id".to_string(), "9942".to_string());

    let ddos_rule = Metadata {
        value: "block".to_string(),
        attributes: ddos_attrs,
    };

    // Insert the malicious /24 infrastructure range into the tree
    let rogue_subnet: IpNetwork = "192.168.1.0/24".parse().unwrap();
    engine.insert(rogue_subnet, ddos_rule).unwrap();

    // 3. Setup a Whitelist rule for a trusted partner API that happens to live inside that subnet
    let mut safe_attrs = HashMap::new();
    safe_attrs.insert("reason".to_string(), "Trusted Partner Payment Gateway".to_string());

    let whitelist_rule = Metadata {
        value: "allow".to_string(),
        attributes: safe_attrs,
    };

    // Insert a highly specific /32 host rule (takes precedence over the /24 due to longest-prefix matching)
    let trusted_host: IpNetwork = "192.168.1.45/32".parse().unwrap();
    engine.insert(trusted_host, whitelist_rule).unwrap();

    println!("------------------------------------------------------------");
    println!("Successfully loaded {} active firewall rule(s).", engine.size());
    println!("------------------------------------------------------------\n");

    // --- SIMULATED TRAFFIC LIFECYCLE ---

    // Scenario A: An attack request comes from a rogue host in the banned subnet
    let attacker_ip: IpAddr = "192.168.1.99".parse().unwrap();
    evaluate_traffic(&engine, attacker_ip);

    // Scenario B: A valid business transaction comes from the whitelisted host inside that same subnet
    let partner_ip: IpAddr = "192.168.1.45".parse().unwrap();
    evaluate_traffic(&engine, partner_ip);

    // Scenario C: Clean traffic from an untracked network address
    let clean_ip: IpAddr = "203.0.113.5".parse().unwrap();
    evaluate_traffic(&engine, clean_ip);
}

/// Helper function simulating how a web server middleware uses your library to inspect IPs
fn evaluate_traffic(engine: &StandardEngine<UncompressedTree>, incoming_ip: IpAddr) {
    print!("Incoming Request from IP: {:<15} -> ", incoming_ip);

    // Query the radixip engine
    // (Assuming your engine handles IpAddr lookup by checking the best-matching network prefix)
    if let Some(metadata) = engine.lookup(&incoming_ip) {
        match metadata.value.as_str() {
            "block" => {
                let reason = metadata.attributes.get("reason").map(|s| s.as_str()).unwrap_or("None");
                println!("❌ DROPPED (403 Forbidden). Reason: {}", reason);
            }
            "allow" => {
                let reason = metadata.attributes.get("reason").map(|s| s.as_str()).unwrap_or("None");
                println!("✅ ALLOWED (Whitelisted). Identity: {}", reason);
            }
            _ => println!("✅ ALLOWED (Unknown Rule Action Passed Through)"),
        }
    } else {
        println!("✅ ALLOWED (No security rules matched, default pass-through)");
    }
}
```