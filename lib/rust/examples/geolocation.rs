use ipnetwork::IpNetwork;
use radixip::{Metadata, NodeVariant, RadixEngine, StandardEngine};

fn main() {
    let engine = StandardEngine::new(NodeVariant::Padded);
    engine
        .insert(
            "203.0.113.0/24".parse::<IpNetwork>().unwrap(),
            Metadata::new("example-region").with_attribute("country", "ZZ"),
        )
        .unwrap();

    println!("loaded {} geolocation prefix(es)", engine.size());
}
