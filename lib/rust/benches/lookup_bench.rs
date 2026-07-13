use criterion::{criterion_group, criterion_main, Criterion};
use radixip_core::RadixEngine;

fn bench_lookup(c: &mut Criterion) {
    let mut engine = RadixEngine::new();
    // Load dataset
    // Run benchmarks
}

criterion_group!(benches, bench_lookup);
criterion_main!(benches);