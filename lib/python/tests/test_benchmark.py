# lib/python/tests/test_benchmark.py
# Run with:
#   pip install pytest pytest-benchmark
#   pytest tests/test_benchmark.py -v --benchmark-autosave

from __future__ import annotations

import ipaddress
import pytest

try:
    from radixip import RadixEngine
except ImportError:
    pytest.skip("radixip native module not built", allow_module_level=True)


# ---------------------------------------------------------------------------
# Dataset helpers
# ---------------------------------------------------------------------------

def generate_cidrs(n: int) -> list[str]:
    cidrs = []
    for i in range(n):
        a = (i // (256 * 256)) % 256
        b = (i // 256) % 256
        c = i % 256
        cidrs.append(f"10.{a}.{b}.{c}/24")
    return cidrs


def generate_hit_ips(n: int) -> list[str]:
    return [
        f"10.{(i // (256*256)) % 256}.{(i // 256) % 256}.{i % 256}"
        for i in range(n)
    ]


def generate_miss_ips(n: int) -> list[str]:
    return [
        f"172.{(i // (256*256)) % 256}.{(i // 256) % 256}.{i % 256}"
        for i in range(n)
    ]


def build_engine(n: int) -> RadixEngine:
    e = RadixEngine()
    meta = {"value": "bench", "attributes": {"type": "benchmark"}}
    for cidr in generate_cidrs(n):
        e.insert(cidr, meta)
    return e


# ---------------------------------------------------------------------------
# Fixtures
# ---------------------------------------------------------------------------

N = 10_000  # keep fast for CI — increase locally for deeper profiling

@pytest.fixture(scope="module")
def engine():
    return build_engine(N)

@pytest.fixture(scope="module")
def hit_ips():
    return generate_hit_ips(N)

@pytest.fixture(scope="module")
def miss_ips():
    return generate_miss_ips(N)

@pytest.fixture(scope="module")
def cidrs():
    return generate_cidrs(N)


# ---------------------------------------------------------------------------
# Benchmarks
# ---------------------------------------------------------------------------

def test_bench_insert(benchmark, cidrs):
    meta = {"value": "bench", "attributes": {}}
    def _insert():
        e = RadixEngine()
        for c in cidrs:
            e.insert(c, meta)
    benchmark(_insert)


def test_bench_lookup_hit(benchmark, engine, hit_ips):
    def _lookup():
        for ip in hit_ips:
            engine.lookup(ip)
    benchmark(_lookup)


def test_bench_lookup_miss(benchmark, engine, miss_ips):
    def _lookup():
        for ip in miss_ips:
            engine.lookup(ip)
    benchmark(_lookup)


def test_bench_size(engine):
    assert len(engine) == N
