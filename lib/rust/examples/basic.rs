use std::net::IpAddr;

use ipnetwork::IpNetwork;
use radixip::tree::UncompressedTree;
use radixip::{Metadata, NodeVariant, RadixEngine, StandardEngine};

fn main() {
    let engine = StandardEngine::new(UncompressedTree::new(NodeVariant::Normal));
    engine
        .insert(
            "10.0.0.0/8".parse::<IpNetwork>().unwrap(),
            Metadata::new("internal"),
        )
        .unwrap();

    let ip = "10.1.2.3".parse::<IpAddr>().unwrap();
    println!("{:?}", engine.lookup(&ip));
}
