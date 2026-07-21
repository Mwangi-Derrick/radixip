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

## Benchmarks

You can verify the performance on your own machine using `pytest-benchmark`:
```bash
cd lib/python
pip install pytest pytest-benchmark
pytest tests/test_benchmark.py
```
