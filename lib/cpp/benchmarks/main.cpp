// lib/cpp/benchmarks/main.cpp
//
// Build and run:
//   g++ -std=c++17 -O3 -I../include main.cpp -L../../../target/release -lradixip -lbenchmark -lpthread -o bench
//   ./bench

#include <benchmark/benchmark.h>
#include <string>
#include <vector>
#include "RadixEngine.hpp"

// ---------------------------------------------------------------------------
// Dataset generators
// ---------------------------------------------------------------------------

std::vector<std::string> generate_cidrs(int n) {
    std::vector<std::string> cidrs;
    cidrs.reserve(n);
    for (int i = 0; i < n; ++i) {
        int a = (i / (256 * 256)) % 256;
        int b = (i / 256) % 256;
        int c = i % 256;
        cidrs.push_back("10." + std::to_string(a) + "." + std::to_string(b) + "." + std::to_string(c) + "/24");
    }
    return cidrs;
}

std::vector<std::string> generate_hit_ips(int n) {
    std::vector<std::string> ips;
    ips.reserve(n);
    for (int i = 0; i < n; ++i) {
        int a = (i / (256 * 256)) % 256;
        int b = (i / 256) % 256;
        int c = i % 256;
        ips.push_back("10." + std::to_string(a) + "." + std::to_string(b) + "." + std::to_string(c));
    }
    return ips;
}

std::vector<std::string> generate_miss_ips(int n) {
    std::vector<std::string> ips;
    ips.reserve(n);
    for (int i = 0; i < n; ++i) {
        int a = (i / (256 * 256)) % 256;
        int b = (i / 256) % 256;
        int c = i % 256;
        ips.push_back("172." + std::to_string(a) + "." + std::to_string(b) + "." + std::to_string(c));
    }
    return ips;
}

radixip::Engine build_engine(int n) {
    radixip::Engine engine;
    auto cidrs = generate_cidrs(n);
    std::unordered_map<std::string, std::string> attrs = {{"type", "benchmark"}};
    for (const auto& cidr : cidrs) {
        engine.insert(cidr, attrs, "bench");
    }
    return engine;
}

// ---------------------------------------------------------------------------
// Benchmarks
// ---------------------------------------------------------------------------

static void BM_Insert(benchmark::State& state) {
    int n = state.range(0);
    auto cidrs = generate_cidrs(n);
    std::unordered_map<std::string, std::string> attrs = {};
    for (auto _ : state) {
        radixip::Engine engine;
        for (const auto& cidr : cidrs) {
            engine.insert(cidr, attrs, "bench");
        }
    }
    state.SetItemsProcessed(state.iterations() * n);
}

static void BM_LookupHit(benchmark::State& state) {
    int n = state.range(0);
    auto engine = build_engine(n);
    auto ips = generate_hit_ips(n);
    for (auto _ : state) {
        for (const auto& ip : ips) {
            auto res = engine.lookup(ip);
            benchmark::DoNotOptimize(res);
        }
    }
    state.SetItemsProcessed(state.iterations() * n);
}

static void BM_LookupMiss(benchmark::State& state) {
    int n = state.range(0);
    auto engine = build_engine(n);
    auto ips = generate_miss_ips(n);
    for (auto _ : state) {
        for (const auto& ip : ips) {
            auto res = engine.lookup(ip);
            benchmark::DoNotOptimize(res);
        }
    }
    state.SetItemsProcessed(state.iterations() * n);
}

// Register benchmarks
BENCHMARK(BM_Insert)->Arg(10000)->Arg(100000);
BENCHMARK(BM_LookupHit)->Arg(10000)->Arg(100000);
BENCHMARK(BM_LookupMiss)->Arg(10000)->Arg(100000);

BENCHMARK_MAIN();
