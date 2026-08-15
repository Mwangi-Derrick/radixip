// lib/rust/benches/lookup_bench.rs
//
// Run with:
//   cargo bench -p radixip-rs
//
// Results are written to: target/criterion/

use criterion::{BenchmarkId, Criterion, Throughput, criterion_group, criterion_main};
use ipnetwork::IpNetwork;
use radixip::{EngineVariant, Metadata, NodeVariant, RadixEngine, engine::EngineWrapper};
use std::net::IpAddr;
use std::str::FromStr;
use std::time::Duration;

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

fn build_engine(
    variant: EngineVariant,
    node_variant: NodeVariant,
    compressed: bool,
    routes: usize,
) -> EngineWrapper {
    let engine = EngineWrapper::new(variant, node_variant, compressed);
    // Simplified metadata to match Go implementation
    let meta = Metadata::new("bench");
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
    // Match Go's 5k only, or keep both? Keeping both for more comprehensive testing
    for &n in &[500usize, 5_000] {
        let cidrs = generate_cidrs(n);
        let meta = Metadata::new("bench");

        group.throughput(Throughput::Elements(n as u64));

        for &nv in &[
            NodeVariant::NormalTrieNode,
            NodeVariant::AtomicTrieNode,
            NodeVariant::PaddedTrieNode,
            NodeVariant::LockFreeTrieNode,
        ] {
            let engine = EngineWrapper::new(EngineVariant::Concurrent, nv, false);
            group.bench_with_input(
                BenchmarkId::new(format!("uncompressed/{nv:?}"), n),
                &n,
                |b, _| {
                    b.iter(|| {
                        for cidr in &cidrs {
                            let _ = criterion::black_box(
                                engine.insert(*cidr, criterion::black_box(meta.clone())),
                            );
                        }
                    });
                },
            );

            // Match compressed variants
            let cnv = match nv {
                NodeVariant::NormalTrieNode => NodeVariant::NormalRadixNode,
                NodeVariant::AtomicTrieNode => NodeVariant::AtomicRadixNode,
                NodeVariant::PaddedTrieNode => NodeVariant::PaddedRadixNode,
                NodeVariant::LockFreeTrieNode => NodeVariant::LockFreeRadixNode,
                _ => nv,
            };
            let engine = EngineWrapper::new(EngineVariant::Concurrent, cnv, true);
            group.bench_with_input(
                BenchmarkId::new(format!("compressed/{cnv:?}"), n),
                &n,
                |b, _| {
                    b.iter(|| {
                        for cidr in &cidrs {
                            let _ = criterion::black_box(
                                engine.insert(*cidr, criterion::black_box(meta.clone())),
                            );
                        }
                    });
                },
            );
            let engine = EngineWrapper::new(EngineVariant::ART, cnv, true);
            // create the ART variant for comparison
            group.bench_with_input(
                BenchmarkId::new(format!("compressed/ART/{cnv:?}"), n),
                &n,
                |b, _| {
                    b.iter(|| {
                        for cidr in &cidrs {
                            let _ = criterion::black_box(
                                engine.insert(*cidr, criterion::black_box(meta.clone())),
                            );
                        }
                    });
                },
            );
        }
    }
    group.finish();
}

// fn bench_lookup_hit(c: &mut Criterion) {
//     let mut group = c.benchmark_group("lookup/hit");
//     // Reduced dataset sizes to 5k, 25k, 50k for memory efficiency
//     for &n in &[5_000usize, 25_000, 50_000] {
//         let ips = generate_ips(n);
//         group.throughput(Throughput::Elements(n as u64));
//         // uncompressed variants
//         for &nv in &[
//             NodeVariant::NormalTrieNode,
//             NodeVariant::AtomicTrieNode,
//             NodeVariant::PaddedTrieNode,
//             NodeVariant::LockFreeTrieNode,
//         ] {
//             let engine_u = build_engine(EngineVariant::Concurrent, nv, false, n);
//             group.bench_with_input(
//                 BenchmarkId::new(format!("uncompressed/{nv:?}"), n),
//                 &n,
//                 |b, _| {
//                     b.iter(|| {
//                         for ip in &ips {
//                             let _ = criterion::black_box(engine_u.lookup(criterion::black_box(ip)));
//                         }
//                     });
//                 },
//             );

//             let cnv = match nv {
//                 NodeVariant::NormalTrieNode => NodeVariant::NormalRadixNode,
//                 NodeVariant::AtomicTrieNode => NodeVariant::AtomicRadixNode,
//                 NodeVariant::PaddedTrieNode => NodeVariant::PaddedRadixNode,
//                 NodeVariant::LockFreeTrieNode => NodeVariant::LockFreeRadixNode,
//                 _ => nv,
//             };

//             let engine_c = build_engine(EngineVariant::Concurrent, cnv, true, n);
//             group.bench_with_input(
//                 BenchmarkId::new(format!("compressed/{cnv:?}"), n),
//                 &n,
//                 |b, _| {
//                     b.iter(|| {
//                         for ip in &ips {
//                             let _ = criterion::black_box(engine_c.lookup(criterion::black_box(ip)));
//                         }
//                     });
//                 },
//             );

//             // create the ART variant for comparison
//             let art_engine = build_engine(EngineVariant::ART, cnv, true, n);
//             group.bench_with_input(
//                 BenchmarkId::new(format!("compressed/ART/{cnv:?}"), n),
//                 &n,
//                 |b, _| {
//                     b.iter(|| {
//                         for ip in &ips {
//                             let _ = criterion::black_box(art_engine.lookup(criterion::black_box(ip)));
//                         }
//                     });
//                 },
//             );
//         }
//     }
//     group.finish();
// }

// fn bench_lookup_miss(c: &mut Criterion) {
//     let mut group = c.benchmark_group("lookup/miss");
//     // Reduced dataset sizes to 5k, 25k, 50k for memory efficiency
//     for &n in &[5_000usize, 25_000, 50_000] {
//         let miss_ips = generate_miss_ips(n);
//         group.throughput(Throughput::Elements(n as u64));
//         // uncompressed variants
//         for &nv in &[
//             NodeVariant::NormalTrieNode,
//             NodeVariant::AtomicTrieNode,
//             NodeVariant::PaddedTrieNode,
//             NodeVariant::LockFreeTrieNode,
//         ] {
//             let engine_u = build_engine(EngineVariant::Concurrent, nv, false, n);
//             group.bench_with_input(
//                 BenchmarkId::new(format!("uncompressed/{nv:?}"), n),
//                 &n,
//                 |b, _| {
//                     b.iter(|| {
//                         for ip in &miss_ips {
//                             let _ = criterion::black_box(engine_u.lookup(criterion::black_box(ip)));
//                         }
//                     });
//                 },
//             );
//             // compressed variants
//             let cnv = match nv {
//                 NodeVariant::NormalTrieNode => NodeVariant::NormalRadixNode,
//                 NodeVariant::AtomicTrieNode => NodeVariant::AtomicRadixNode,
//                 NodeVariant::PaddedTrieNode => NodeVariant::PaddedRadixNode,
//                 NodeVariant::LockFreeTrieNode => NodeVariant::LockFreeRadixNode,
//                 _ => nv,
//             };

//             let engine_c = build_engine(EngineVariant::Concurrent, cnv, true, n);
//             group.bench_with_input(
//                 BenchmarkId::new(format!("compressed/{cnv:?}"), n),
//                 &n,
//                 |b, _| {
//                     b.iter(|| {
//                         for ip in &miss_ips {
//                             let _ = criterion::black_box(engine_c.lookup(criterion::black_box(ip)));
//                         }
//                     });
//                 },
//             );
//             // create the ART variant for comparison
//             let art_engine = build_engine(EngineVariant::ART, cnv, true, n);
//             group.bench_with_input(
//                 BenchmarkId::new(format!("compressed/ART/{cnv:?}"), n),
//                 &n,
//                 |b, _| {
//                     b.iter(|| {
//                         for ip in &miss_ips {
//                             let _ = criterion::black_box(art_engine.lookup(criterion::black_box(ip)));
//                         }
//                     });
//                 },
//             );
//         }
//     }
//     group.finish();
// }

// ---------------------------------------------------------------------------
// Concurrent Lookup Benchmarks - Compressed
// ---------------------------------------------------------------------------

fn bench_concurrent_lookup_compressed(c: &mut Criterion) {
    use std::sync::Arc;
    use std::thread;

    let mut group = c.benchmark_group("concurrent_lookup/compressed");
    let n = 25_000usize;
    let ips = Arc::new(generate_ips(n));

    for &nv in &[
        NodeVariant::NormalRadixNode,
        NodeVariant::AtomicRadixNode,
        NodeVariant::PaddedRadixNode,
        NodeVariant::LockFreeRadixNode,
    ] {
        let engine = Arc::new(build_engine(EngineVariant::Concurrent, nv, true, n));
        group.throughput(Throughput::Elements((n * 4) as u64)); // 4 threads
        group.bench_function(format!("4_threads/{nv:?}"), |b| {
            b.iter(|| {
                let handles: Vec<_> = (0..4)
                    .map(|_| {
                        let e = Arc::clone(&engine);
                        let ips = Arc::clone(&ips);
                        thread::spawn(move || {
                            for ip in ips.iter() {
                                let _ = criterion::black_box(e.lookup(criterion::black_box(ip)));
                            }
                        })
                    })
                    .collect();
                for h in handles {
                    h.join().unwrap();
                }
            });
        });
    }
    group.finish();
}

//  Bench Concurrent ART Lookup

fn bench_concurrent_lookup_art(c: &mut Criterion) {
    use std::sync::Arc;
    use std::thread;

    let mut group = c.benchmark_group("concurrent_lookup/art");
    let n = 25_000usize;
    let ips = Arc::new(generate_ips(n));

    let engine = Arc::new(build_engine(
        EngineVariant::ART,
        NodeVariant::NormalRadixNode,
        true,
        n,
    ));
    group.throughput(Throughput::Elements((n * 4) as u64)); // 4 threads
    group.bench_function("4_threads/ART", |b| {
        b.iter(|| {
            let handles: Vec<_> = (0..4)
                .map(|_| {
                    let e = Arc::clone(&engine);
                    let ips = Arc::clone(&ips);
                    thread::spawn(move || {
                        for ip in ips.iter() {
                            let _ = criterion::black_box(e.lookup(criterion::black_box(ip)));
                        }
                    })
                })
                .collect();
            for h in handles {
                h.join().unwrap();
            }
        });
    });
    group.finish();
}

// ---------------------------------------------------------------------------
// Concurrent Lookup Benchmarks - Uncompressed (NEW to match Go)
// ---------------------------------------------------------------------------

fn bench_concurrent_lookup_uncompressed(c: &mut Criterion) {
    use std::sync::Arc;
    use std::thread;

    let mut group = c.benchmark_group("concurrent_lookup/uncompressed");
    let n = 25_000usize;
    let ips = Arc::new(generate_ips(n));

    for &nv in &[
        NodeVariant::NormalTrieNode,
        NodeVariant::AtomicTrieNode,
        NodeVariant::PaddedTrieNode,
        NodeVariant::LockFreeTrieNode,
    ] {
        let engine = Arc::new(build_engine(EngineVariant::Concurrent, nv, false, n));
        group.throughput(Throughput::Elements((n * 4) as u64)); // 4 threads
        group.bench_function(format!("4_threads/{nv:?}"), |b| {
            b.iter(|| {
                let handles: Vec<_> = (0..4)
                    .map(|_| {
                        let e = Arc::clone(&engine);
                        let ips = Arc::clone(&ips);
                        thread::spawn(move || {
                            for ip in ips.iter() {
                                let _ = criterion::black_box(e.lookup(criterion::black_box(ip)));
                            }
                        })
                    })
                    .collect();
                for h in handles {
                    h.join().unwrap();
                }
            });
        });
    }
    group.finish();
}

// ---------------------------------------------------------------------------
// Criterion Configuration for CI: reduced sampling for memory efficiency
// ---------------------------------------------------------------------------
fn config() -> Criterion {
    Criterion::default()
        .sample_size(20) // Lower from default 100
        .measurement_time(Duration::from_secs(8)) // Lower from default 5s
        .warm_up_time(Duration::from_secs(1)) // Lower from default 3s
        .without_plots() // Skip expensive graph generation
}

criterion_group! {
    name = benches;
    config = config();
    targets =
        bench_insert,
        // bench_lookup_hit,
        // bench_lookup_miss,
        bench_concurrent_lookup_compressed,
        bench_concurrent_lookup_art,
        bench_concurrent_lookup_uncompressed
}

criterion_main!(benches);
