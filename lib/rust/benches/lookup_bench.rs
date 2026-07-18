// lib/rust/benches/lookup_bench.rs
//
// Run with:
//   cargo bench -p radixip-rs
//
// Results are written to: target/criterion/

use criterion::{criterion_group, criterion_main, BenchmarkId, Criterion, Throughput};
use ipnetwork::IpNetwork;
use radixip::{
    EngineVariant, Metadata, NodeVariant, RadixConfig, RadixEngine,
    engine::EngineWrapper,
};
use std::net::IpAddr;
use std::str::FromStr;

// ---------------------------------------------------------------------------
// Dataset generators
// ---------------------------------------------------------------------------

/// Generate N realistic-looking /24 CIDR blocks spread across the 10.x.x.0/24 space.
fn generate_cidrs(n: usize) -> Vec<IpNetwork> {
    (0..n)
        .map(|i| {
            let a = (i / (256 * 256)) % 256;
            let b = (i / 256) % 256;
            let c = i % 256;
            format!("10.{a}.{b}.{c}/24").parse().unwrap()
        })
        .collect()
}

/// Generate N IP addresses that are guaranteed to hit the inserted /24 blocks.
fn generate_ips(n: usize) -> Vec<IpAddr> {
    (0..n)
        .map(|i| {
            let a = (i / (256 * 256)) % 256;
            let b = (i / 256) % 256;
            let c = i % 256;
            IpAddr::from_str(&format!("10.{a}.{b}.{c}")).unwrap()
        })
        .collect()
}

/// Generate N IP addresses that are guaranteed NOT to match (cold misses).
fn generate_miss_ips(n: usize) -> Vec<IpAddr> {
    (0..n)
        .map(|i| {
            let a = (i / (256 * 256)) % 256;
            let b = (i / 256) % 256;
            let c = i % 256;
            IpAddr::from_str(&format!("172.{a}.{b}.{c}")).unwrap()
        })
        .collect()
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

fn build_engine(variant: EngineVariant, compressed: bool, routes: usize) -> EngineWrapper {
    let engine = EngineWrapper::new(variant, NodeVariant::Atomic, compressed);
    let meta = Metadata::new("bench").with_attribute("type", "benchmark");
    for cidr in generate_cidrs(routes) {
        let _ = engine.insert(cidr, meta.clone());
    }
    engine
}

// ---------------------------------------------------------------------------
// Benchmarks
// ---------------------------------------------------------------------------

fn bench_insert(c: &mut Criterion) {
    let mut group = c.benchmark_group("insert");
    for &n in &[1_000usize, 10_000, 100_000] {
        let cidrs = generate_cidrs(n);
        let meta = Metadata::new("bench");

        group.throughput(Throughput::Elements(n as u64));
        group.bench_with_input(BenchmarkId::new("uncompressed", n), &n, |b, _| {
            b.iter(|| {
                let engine = EngineWrapper::new(EngineVariant::Concurrent, NodeVariant::Atomic, false);
                for cidr in &cidrs {
                    let _ = engine.insert(*cidr, meta.clone());
                }
            });
        });
        group.bench_with_input(BenchmarkId::new("compressed", n), &n, |b, _| {
            b.iter(|| {
                let engine = EngineWrapper::new(EngineVariant::Concurrent, NodeVariant::Atomic, true);
                for cidr in &cidrs {
                    let _ = engine.insert(*cidr, meta.clone());
                }
            });
        });
    }
    group.finish();
}

fn bench_lookup_hit(c: &mut Criterion) {
    let mut group = c.benchmark_group("lookup/hit");
    for &n in &[10_000usize, 100_000, 500_000] {
        let ips = generate_ips(n);
        let engine_u = build_engine(EngineVariant::Concurrent, false, n);
        let engine_c = build_engine(EngineVariant::Concurrent, true, n);

        group.throughput(Throughput::Elements(n as u64));
        group.bench_with_input(BenchmarkId::new("uncompressed", n), &n, |b, _| {
            b.iter(|| {
                for ip in &ips {
                    let _ = engine_u.lookup(ip);
                }
            });
        });
        group.bench_with_input(BenchmarkId::new("compressed", n), &n, |b, _| {
            b.iter(|| {
                for ip in &ips {
                    let _ = engine_c.lookup(ip);
                }
            });
        });
    }
    group.finish();
}

fn bench_lookup_miss(c: &mut Criterion) {
    let mut group = c.benchmark_group("lookup/miss");
    for &n in &[10_000usize, 100_000] {
        let miss_ips = generate_miss_ips(n);
        let engine_u = build_engine(EngineVariant::Concurrent, false, n);
        let engine_c = build_engine(EngineVariant::Concurrent, true, n);

        group.throughput(Throughput::Elements(n as u64));
        group.bench_with_input(BenchmarkId::new("uncompressed", n), &n, |b, _| {
            b.iter(|| {
                for ip in &miss_ips {
                    let _ = engine_u.lookup(ip);
                }
            });
        });
        group.bench_with_input(BenchmarkId::new("compressed", n), &n, |b, _| {
            b.iter(|| {
                for ip in &miss_ips {
                    let _ = engine_c.lookup(ip);
                }
            });
        });
    }
    group.finish();
}

fn bench_concurrent_lookup(c: &mut Criterion) {
    use std::sync::Arc;
    use std::thread;

    let mut group = c.benchmark_group("concurrent_lookup");
    let n = 50_000usize;
    let ips = Arc::new(generate_ips(n));
    let engine = Arc::new(build_engine(EngineVariant::Concurrent, true, n));

    group.throughput(Throughput::Elements((n * 4) as u64)); // 4 threads
    group.bench_function("4_threads_compressed", |b| {
        b.iter(|| {
            let handles: Vec<_> = (0..4)
                .map(|_| {
                    let e = Arc::clone(&engine);
                    let ips = Arc::clone(&ips);
                    thread::spawn(move || {
                        for ip in ips.iter() {
                            let _ = e.lookup(ip);
                        }
                    })
                })
                .collect();
            for h in handles { h.join().unwrap(); }
        });
    });
    group.finish();
}

criterion_group!(
    benches,
    bench_insert,
    bench_lookup_hit,
    bench_lookup_miss,
    bench_concurrent_lookup,
);
criterion_main!(benches);