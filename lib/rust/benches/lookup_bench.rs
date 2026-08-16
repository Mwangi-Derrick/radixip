// lib/rust/benches/lookup_bench.rs
//
// Run with:
//   cargo bench -p radixip-rs
//
// Results are written to: target/criterion/

use criterion::{BatchSize, BenchmarkId, Criterion, Throughput, criterion_group, criterion_main};
use ipnetwork::IpNetwork;
use radixip::{EngineVariant, Metadata, NodeVariant, RadixEngine, engine::EngineWrapper};
use std::net::IpAddr;
use std::str::FromStr;
use std::time::Duration;

// ---------------------------------------------------------------------------
// Dataset generators
// ---------------------------------------------------------------------------

/// Generate N realistic-looking /24 CIDR blocks spread across the 10.x.x.0/24 space (IPv4).
fn generate_cidrs_ipv4(n: usize) -> Vec<IpNetwork> {
    (0..n)
        .map(|i| {
            let a = (i / (256 * 256)) % 256;
            let b = (i / 256) % 256;
            let c = i % 256;
            format!("10.{a}.{b}.{c}/24").parse().unwrap()
        })
        .collect()
}

/// Generate N realistic-looking /64 CIDR blocks spread across the fd00::/8 ULA space (IPv6).
fn generate_cidrs_ipv6(n: usize) -> Vec<IpNetwork> {
    (0..n)
        .map(|i| {
            let low = (i % 65536) as u16;
            let high = (i / 65536) as u16;
            format!(
                "fd{:02x}:{:04x}:{:04x}::/64",
                (i / (65536 * 65536)) % 256,
                high,
                low
            )
            .parse()
            .unwrap()
        })
        .collect()
}

/// Generate N IPv4 addresses that are guaranteed to hit the inserted /24 blocks.
fn generate_ips_ipv4(n: usize) -> Vec<IpAddr> {
    (0..n)
        .map(|i| {
            let a = (i / (256 * 256)) % 256;
            let b = (i / 256) % 256;
            let c = i % 256;
            IpAddr::from_str(&format!("10.{a}.{b}.{c}")).unwrap()
        })
        .collect()
}

/// Generate N IPv6 addresses that are guaranteed to hit the inserted /64 blocks.
fn generate_ips_ipv6(n: usize) -> Vec<IpAddr> {
    (0..n)
        .map(|i| {
            let low = (i % 65536) as u16;
            let high = (i / 65536) as u16;
            format!(
                "fd{:02x}:{:04x}:{:04x}:{:04x}:dead:beef:cafe:feed",
                (i / (65536 * 65536)) % 256,
                high,
                low,
                (i % 256) as u16
            )
            .parse()
            .unwrap()
        })
        .collect()
}

/// Generate N IPv4 addresses that are guaranteed NOT to match (cold misses).
#[allow(dead_code)]
fn generate_miss_ips_ipv4(n: usize) -> Vec<IpAddr> {
    (0..n)
        .map(|i| {
            let a = (i / (256 * 256)) % 256;
            let b = (i / 256) % 256;
            let c = i % 256;
            IpAddr::from_str(&format!("172.{a}.{b}.{c}")).unwrap()
        })
        .collect()
}

/// Generate N IPv6 addresses that are guaranteed NOT to match (cold misses).
#[allow(dead_code)]
fn generate_miss_ips_ipv6(n: usize) -> Vec<IpAddr> {
    (0..n)
        .map(|i| {
            let low = (i % 65536) as u16;
            let high = (i / 65536) as u16;
            format!("2001:db8:{:04x}:{:04x}:dead:beef:cafe:feed", high, low)
                .parse()
                .unwrap()
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
    ipv6: bool,
) -> EngineWrapper {
    let engine = EngineWrapper::new(variant, node_variant, compressed);
    let meta = Metadata::new("bench");

    let cidrs = if ipv6 {
        generate_cidrs_ipv6(routes)
    } else {
        generate_cidrs_ipv4(routes)
    };

    for cidr in cidrs {
        let _ = engine.insert(cidr, meta.clone());
    }
    engine
}

fn compressed_variant(node_variant: NodeVariant) -> NodeVariant {
    match node_variant {
        NodeVariant::NormalTrieNode | NodeVariant::NormalRadixNode => NodeVariant::NormalRadixNode,
        NodeVariant::AtomicTrieNode | NodeVariant::AtomicRadixNode => NodeVariant::AtomicRadixNode,
        NodeVariant::PaddedTrieNode | NodeVariant::PaddedRadixNode => NodeVariant::PaddedRadixNode,
        NodeVariant::LockFreeTrieNode | NodeVariant::LockFreeRadixNode => {
            NodeVariant::LockFreeRadixNode
        }
    }
}

// ---------------------------------------------------------------------------
// Benchmarks
// ---------------------------------------------------------------------------

fn bench_insert(c: &mut Criterion) {
    let mut group = c.benchmark_group("insert");

    for &n in &[500usize, 5_000] {
        let cidrs_ipv4 = generate_cidrs_ipv4(n);
        let cidrs_ipv6 = generate_cidrs_ipv6(n);
        let meta = Metadata::new("bench");

        group.throughput(Throughput::Elements(n as u64));

        for &nv in &[
            NodeVariant::NormalTrieNode,
            NodeVariant::AtomicTrieNode,
            NodeVariant::PaddedTrieNode,
            NodeVariant::LockFreeTrieNode,
        ] {
            // IPv4 Benchmarks
            group.bench_with_input(
                BenchmarkId::new(format!("ipv4/uncompressed/{nv:?}"), n),
                &n,
                |b, _| {
                    b.iter_batched(
                        || EngineWrapper::new(EngineVariant::Concurrent, nv, false),
                        |engine| {
                            for cidr in &cidrs_ipv4 {
                                let _ = criterion::black_box(
                                    engine.insert(*cidr, criterion::black_box(meta.clone())),
                                );
                            }
                        },
                        BatchSize::SmallInput,
                    );
                },
            );

            let cnv = compressed_variant(nv);
            group.bench_with_input(
                BenchmarkId::new(format!("ipv4/compressed/{cnv:?}"), n),
                &n,
                |b, _| {
                    b.iter_batched(
                        || EngineWrapper::new(EngineVariant::Concurrent, cnv, true),
                        |engine| {
                            for cidr in &cidrs_ipv4 {
                                let _ = criterion::black_box(
                                    engine.insert(*cidr, criterion::black_box(meta.clone())),
                                );
                            }
                        },
                        BatchSize::SmallInput,
                    );
                },
            );
            group.bench_with_input(
                BenchmarkId::new(format!("ipv4/compressed/ART/{cnv:?}"), n),
                &n,
                |b, _| {
                    b.iter_batched(
                        || EngineWrapper::new(EngineVariant::ART, cnv, true),
                        |engine| {
                            for cidr in &cidrs_ipv4 {
                                let _ = criterion::black_box(
                                    engine.insert(*cidr, criterion::black_box(meta.clone())),
                                );
                            }
                        },
                        BatchSize::SmallInput,
                    );
                },
            );

            // IPv6 Benchmarks
            group.bench_with_input(
                BenchmarkId::new(format!("ipv6/uncompressed/{nv:?}"), n),
                &n,
                |b, _| {
                    b.iter_batched(
                        || EngineWrapper::new(EngineVariant::Concurrent, nv, false),
                        |engine| {
                            for cidr in &cidrs_ipv6 {
                                let _ = criterion::black_box(
                                    engine.insert(*cidr, criterion::black_box(meta.clone())),
                                );
                            }
                        },
                        BatchSize::SmallInput,
                    );
                },
            );

            group.bench_with_input(
                BenchmarkId::new(format!("ipv6/compressed/{cnv:?}"), n),
                &n,
                |b, _| {
                    b.iter_batched(
                        || EngineWrapper::new(EngineVariant::Concurrent, cnv, true),
                        |engine| {
                            for cidr in &cidrs_ipv6 {
                                let _ = criterion::black_box(
                                    engine.insert(*cidr, criterion::black_box(meta.clone())),
                                );
                            }
                        },
                        BatchSize::SmallInput,
                    );
                },
            );
            group.bench_with_input(
                BenchmarkId::new(format!("ipv6/compressed/ART/{cnv:?}"), n),
                &n,
                |b, _| {
                    b.iter_batched(
                        || EngineWrapper::new(EngineVariant::ART, cnv, true),
                        |engine| {
                            for cidr in &cidrs_ipv6 {
                                let _ = criterion::black_box(
                                    engine.insert(*cidr, criterion::black_box(meta.clone())),
                                );
                            }
                        },
                        BatchSize::SmallInput,
                    );
                },
            );
        }
    }
    group.finish();
}

fn bench_lookup(c: &mut Criterion) {
    let mut group = c.benchmark_group("lookup");

    for &n in &[500usize, 5_000] {
        let cidrs_ipv4 = generate_cidrs_ipv4(n);
        let cidrs_ipv6 = generate_cidrs_ipv6(n);
        let meta = Metadata::new("bench");

        group.throughput(Throughput::Elements(n as u64));

        for &nv in &[
            NodeVariant::NormalTrieNode,
            NodeVariant::AtomicTrieNode,
            NodeVariant::PaddedTrieNode,
            NodeVariant::LockFreeTrieNode,
        ] {
            // Pre-populate engines for IPv4
            let engines_ipv4: Vec<_> = (0..4)
                .map(|_| {
                    let engine = EngineWrapper::new(EngineVariant::Concurrent, nv, false);
                    for cidr in &cidrs_ipv4 {
                        let _ = engine.insert(*cidr, meta.clone());
                    }
                    engine
                })
                .collect();

            // IPv4 Benchmarks - Uncompressed
            group.bench_with_input(
                BenchmarkId::new(format!("ipv4/uncompressed/{:?}", nv), n),
                &n,
                |b, _| {
                    b.iter_batched(
                        || engines_ipv4[rand::random::<usize>() % 4].clone(),
                        |engine| {
                            for cidr in &cidrs_ipv4 {
                                let _ = criterion::black_box(
                                    engine.lookup(criterion::black_box(*cidr)),
                                );
                            }
                        },
                        BatchSize::SmallInput,
                    );
                },
            );

            // IPv4 Benchmarks - Compressed
            let cnv = compressed_variant(nv);
            let engines_ipv4_compressed: Vec<_> = (0..4)
                .map(|_| {
                    let engine = EngineWrapper::new(EngineVariant::Concurrent, cnv, true);
                    for cidr in &cidrs_ipv4 {
                        let _ = engine.insert(*cidr, meta.clone());
                    }
                    engine
                })
                .collect();

            group.bench_with_input(
                BenchmarkId::new(format!("ipv4/compressed/{:?}", cnv), n),
                &n,
                |b, _| {
                    b.iter_batched(
                        || engines_ipv4_compressed[rand::random::<usize>() % 4].clone(),
                        |engine| {
                            for cidr in &cidrs_ipv4 {
                                let _ = criterion::black_box(
                                    engine.lookup(criterion::black_box(*cidr)),
                                );
                            }
                        },
                        BatchSize::SmallInput,
                    );
                },
            );

            // IPv4 Benchmarks - Compressed ART
            let engines_ipv4_art: Vec<_> = (0..4)
                .map(|_| {
                    let engine = EngineWrapper::new(EngineVariant::ART, cnv, true);
                    for cidr in &cidrs_ipv4 {
                        let _ = engine.insert(*cidr, meta.clone());
                    }
                    engine
                })
                .collect();

            group.bench_with_input(
                BenchmarkId::new(format!("ipv4/compressed/ART/{:?}", cnv), n),
                &n,
                |b, _| {
                    b.iter_batched(
                        || engines_ipv4_art[rand::random::<usize>() % 4].clone(),
                        |engine| {
                            for cidr in &cidrs_ipv4 {
                                let _ = criterion::black_box(
                                    engine.lookup(criterion::black_box(*cidr)),
                                );
                            }
                        },
                        BatchSize::SmallInput,
                    );
                },
            );

            // Pre-populate engines for IPv6
            let engines_ipv6: Vec<_> = (0..4)
                .map(|_| {
                    let engine = EngineWrapper::new(EngineVariant::Concurrent, nv, false);
                    for cidr in &cidrs_ipv6 {
                        let _ = engine.insert(*cidr, meta.clone());
                    }
                    engine
                })
                .collect();

            // IPv6 Benchmarks - Uncompressed
            group.bench_with_input(
                BenchmarkId::new(format!("ipv6/uncompressed/{:?}", nv), n),
                &n,
                |b, _| {
                    b.iter_batched(
                        || engines_ipv6[rand::random::<usize>() % 4].clone(),
                        |engine| {
                            for cidr in &cidrs_ipv6 {
                                let _ = criterion::black_box(
                                    engine.lookup(criterion::black_box(*cidr)),
                                );
                            }
                        },
                        BatchSize::SmallInput,
                    );
                },
            );

            // IPv6 Benchmarks - Compressed
            let engines_ipv6_compressed: Vec<_> = (0..4)
                .map(|_| {
                    let engine = EngineWrapper::new(EngineVariant::Concurrent, cnv, true);
                    for cidr in &cidrs_ipv6 {
                        let _ = engine.insert(*cidr, meta.clone());
                    }
                    engine
                })
                .collect();

            group.bench_with_input(
                BenchmarkId::new(format!("ipv6/compressed/{:?}", cnv), n),
                &n,
                |b, _| {
                    b.iter_batched(
                        || engines_ipv6_compressed[rand::random::<usize>() % 4].clone(),
                        |engine| {
                            for cidr in &cidrs_ipv6 {
                                let _ = criterion::black_box(
                                    engine.lookup(criterion::black_box(*cidr)),
                                );
                            }
                        },
                        BatchSize::SmallInput,
                    );
                },
            );

            // IPv6 Benchmarks - Compressed ART
            let engines_ipv6_art: Vec<_> = (0..4)
                .map(|_| {
                    let engine = EngineWrapper::new(EngineVariant::ART, cnv, true);
                    for cidr in &cidrs_ipv6 {
                        let _ = engine.insert(*cidr, meta.clone());
                    }
                    engine
                })
                .collect();

            group.bench_with_input(
                BenchmarkId::new(format!("ipv6/compressed/ART/{:?}", cnv), n),
                &n,
                |b, _| {
                    b.iter_batched(
                        || engines_ipv6_art[rand::random::<usize>() % 4].clone(),
                        |engine| {
                            for cidr in &cidrs_ipv6 {
                                let _ = criterion::black_box(
                                    engine.lookup(criterion::black_box(*cidr)),
                                );
                            }
                        },
                        BatchSize::SmallInput,
                    );
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
    let ips_ipv4 = Arc::new(generate_ips_ipv4(n));
    let ips_ipv6 = Arc::new(generate_ips_ipv6(n));

    for &nv in &[
        NodeVariant::NormalRadixNode,
        NodeVariant::AtomicRadixNode,
        NodeVariant::PaddedRadixNode,
        NodeVariant::LockFreeRadixNode,
    ] {
        // IPv4
        let engine = Arc::new(build_engine(EngineVariant::Concurrent, nv, true, n, false));
        group.throughput(Throughput::Elements((n * 4) as u64));
        group.bench_function(format!("ipv4/4_threads/{nv:?}"), |b| {
            b.iter(|| {
                let handles: Vec<_> = (0..4)
                    .map(|_| {
                        let e = Arc::clone(&engine);
                        let ips = Arc::clone(&ips_ipv4);
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

        // IPv6
        let engine = Arc::new(build_engine(EngineVariant::Concurrent, nv, true, n, true));
        group.bench_function(format!("ipv6/4_threads/{nv:?}"), |b| {
            b.iter(|| {
                let handles: Vec<_> = (0..4)
                    .map(|_| {
                        let e = Arc::clone(&engine);
                        let ips = Arc::clone(&ips_ipv6);
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
// Concurrent ART Lookup
// ---------------------------------------------------------------------------

fn bench_concurrent_lookup_art(c: &mut Criterion) {
    use std::sync::Arc;
    use std::thread;

    let mut group = c.benchmark_group("concurrent_lookup/art");
    let n = 25_000usize;
    let ips_ipv4 = Arc::new(generate_ips_ipv4(n));
    let ips_ipv6 = Arc::new(generate_ips_ipv6(n));

    // IPv4
    let engine = Arc::new(build_engine(
        EngineVariant::ART,
        NodeVariant::NormalRadixNode,
        true,
        n,
        false,
    ));
    group.throughput(Throughput::Elements((n * 4) as u64));
    group.bench_function("ipv4/4_threads/ART", |b| {
        b.iter(|| {
            let handles: Vec<_> = (0..4)
                .map(|_| {
                    let e = Arc::clone(&engine);
                    let ips = Arc::clone(&ips_ipv4);
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

    // IPv6
    let engine = Arc::new(build_engine(
        EngineVariant::ART,
        NodeVariant::NormalRadixNode,
        true,
        n,
        true,
    ));
    group.bench_function("ipv6/4_threads/ART", |b| {
        b.iter(|| {
            let handles: Vec<_> = (0..4)
                .map(|_| {
                    let e = Arc::clone(&engine);
                    let ips = Arc::clone(&ips_ipv6);
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
// Concurrent Lookup Benchmarks - Uncompressed
// ---------------------------------------------------------------------------

fn bench_concurrent_lookup_uncompressed(c: &mut Criterion) {
    use std::sync::Arc;
    use std::thread;

    let mut group = c.benchmark_group("concurrent_lookup/uncompressed");
    let n = 25_000usize;
    let ips_ipv4 = Arc::new(generate_ips_ipv4(n));
    let ips_ipv6 = Arc::new(generate_ips_ipv6(n));

    for &nv in &[
        NodeVariant::NormalTrieNode,
        NodeVariant::AtomicTrieNode,
        NodeVariant::PaddedTrieNode,
        NodeVariant::LockFreeTrieNode,
    ] {
        // IPv4
        let engine = Arc::new(build_engine(EngineVariant::Concurrent, nv, false, n, false));
        group.throughput(Throughput::Elements((n * 4) as u64));
        group.bench_function(format!("ipv4/4_threads/{nv:?}"), |b| {
            b.iter(|| {
                let handles: Vec<_> = (0..4)
                    .map(|_| {
                        let e = Arc::clone(&engine);
                        let ips = Arc::clone(&ips_ipv4);
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

        // IPv6
        let engine = Arc::new(build_engine(EngineVariant::Concurrent, nv, false, n, true));
        group.bench_function(format!("ipv6/4_threads/{nv:?}"), |b| {
            b.iter(|| {
                let handles: Vec<_> = (0..4)
                    .map(|_| {
                        let e = Arc::clone(&engine);
                        let ips = Arc::clone(&ips_ipv6);
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
// Criterion Configuration
// ---------------------------------------------------------------------------
fn config() -> Criterion {
    let criterion = Criterion::default().without_plots();

    if std::env::var_os("CI").is_some() {
        criterion
            .sample_size(10)
            .measurement_time(Duration::from_millis(1500))
            .warm_up_time(Duration::from_millis(100))
    } else {
        criterion
            .sample_size(20)
            .measurement_time(Duration::from_secs(5))
            .warm_up_time(Duration::from_secs(1))
    }
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
