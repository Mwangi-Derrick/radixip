# RadixIP Python Bindings

High-performance IP longest-prefix matching for Python, powered by a native Rust backend via `PyO3`.

## Why RadixIP in Python?

Python is notoriously bound by the Global Interpreter Lock (GIL). When dealing with network engineering tasks (like parsing millions of BGP routes or massive log files), native Python dictionaries and classes can quickly become a memory and CPU bottleneck.

RadixIP pushes the entire routing table and bit-traversal logic down into native Rust memory. Because of the GIL, the engine is explicitly configured to use the memory-efficient `StandardEngine` (without threading shards) to maximize single-core throughput.

## Installation

```bash
pip install radixip
```
Pre-built wheels are distributed for Windows, Linux, and macOS (x86_64 & ARM64).

## Usage

```python
from radixip import RadixEngine

# Initialize the native engine
engine = RadixEngine()

# Insert prefixes with rich metadata
engine.insert("10.0.0.0/8", { 
    "value": "allow", 
    "attributes": { "asn": "AS12345", "region": "us-east" } 
})

# Perform sub-microsecond longest-prefix matching
match = engine.lookup("10.1.2.3")

if match:
    print(f"Matched! Action: {match['value']}")
    print(f"Attributes:", match['attributes'])
else:
    print("No match found.")

# Statistics
print(engine.stats())
```

## Policy checks

The native policy binding loads the same YAML configuration used by the Rust
and Go middleware. Token buckets, blocklist checks, and auto-ban state remain
in Rust; Python receives only the compact decision result.

```python
from radixip import RadixPolicy

policy = RadixPolicy.from_yaml("config/radixip.yaml")
result = policy.check_ip("203.0.113.10")
print(result["decision"])             # allow, block, or limit
print(result["retry_after_seconds"])
```

Use this result in Flask, Django, FastAPI, or another Python framework
middleware. Keep one policy instance per process rather than constructing one
per request.

## FastAPI middleware

```python
from fastapi import FastAPI
from radixip import RadixPolicy
from radixip.middleware import RadixIPMiddleware

app = FastAPI()
policy = RadixPolicy.from_yaml("config/radixip.yaml")
app.add_middleware(RadixIPMiddleware, policy=policy)
```

The default uses the direct peer address and does not trust forwarded headers.
Pass `resolve_ip` to `RadixIPMiddleware` when the application sits behind a
known, trusted proxy chain.

## Benchmarks

You can verify the performance on your own machine using `pytest-benchmark`:
```bash
cd lib/python
pip install pytest pytest-benchmark
pytest tests/test_benchmark.py
```
