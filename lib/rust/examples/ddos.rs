use ipnetwork::IpNetwork;
use radixip::{Metadata, NodeVariant, RadixEngine, StandardEngine};

fn main() {
    let engine = StandardEngine::new(NodeVariant::Atomic);
    engine
        .insert(
            "192.168.1.0/24".parse::<IpNetwork>().unwrap(),
            Metadata::new("block"),
        )
        .unwrap();

    println!("loaded {} DDoS rule(s)", engine.size());
}
