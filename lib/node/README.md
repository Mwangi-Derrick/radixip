# RadixIP Node.js Bindings

High-performance IP longest-prefix matching for Node.js, powered by a native Rust backend via `napi-rs`.

## Why RadixIP in Node.js?

Node.js operates on a single-threaded Event Loop. When you need to parse millions of IP addresses per second (e.g., in an Express middleware, API Gateway, or DDoS mitigation script), pure JavaScript Hash Maps and Arrays trigger massive Garbage Collection (GC) pauses.

RadixIP pushes the entire routing table and bit-traversal logic down into native Rust memory. The JavaScript thread only pays a tiny FFI boundary cost, completely bypassing V8 Garbage Collection for the data structure itself.

> **Note on Concurrency**: Because Node.js is single-threaded, RadixIP is configured by default to use the `StandardEngine` with a lock-free compressed Patricia tree. Sharding is disabled to prevent unnecessary mutex locking overhead.

## Installation

```bash
npm install radixip
```
Pre-built binaries are distributed for Windows, Linux, and macOS (x86_64 & ARM64). You don't need a Rust compiler to install it!

## Usage

```typescript
import { RadixIP } from 'radixip';

// Initialize the native engine
const engine = new RadixIP();

// Insert prefixes with rich metadata
engine.insert('10.0.0.0/8', { 
    value: 'allow', 
    attributes: { asn: 'AS12345', region: 'us-east' } 
});

// Perform sub-microsecond longest-prefix matching
const match = engine.lookup('10.1.2.3');

if (match) {
    console.log(`Matched! Action: ${match.value}`);
    console.log(`Attributes:`, match.attributes);
} else {
    console.log('No match found.');
}
```

## Benchmarks

You can verify the performance on your own machine:
```bash
cd lib/node
npm run build
node benchmarks/bench.js
```
