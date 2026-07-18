// lib/node/benchmarks/bench.js
// Run with: node benchmarks/bench.js
// Requires the native addon to be built first: npm run build

'use strict';

const { RadixIP } = require('..');
const { performance } = require('perf_hooks');

// ---------------------------------------------------------------------------
// Dataset
// ---------------------------------------------------------------------------

function generateCIDRs(n) {
  const cidrs = [];
  for (let i = 0; i < n; i++) {
    const a = Math.floor(i / (256 * 256)) % 256;
    const b = Math.floor(i / 256) % 256;
    const c = i % 256;
    cidrs.push(`10.${a}.${b}.${c}/24`);
  }
  return cidrs;
}

function generateHitIPs(n) {
  return Array.from({ length: n }, (_, i) => {
    const a = Math.floor(i / (256 * 256)) % 256;
    const b = Math.floor(i / 256) % 256;
    const c = i % 256;
    return `10.${a}.${b}.${c}`;
  });
}

function generateMissIPs(n) {
  return Array.from({ length: n }, (_, i) => {
    const a = Math.floor(i / (256 * 256)) % 256;
    const b = Math.floor(i / 256) % 256;
    const c = i % 256;
    return `172.${a}.${b}.${c}`;
  });
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function buildEngine(n) {
  const engine = new RadixIP();
  const cidrs = generateCIDRs(n);
  const meta = { value: 'bench', attributes: { type: 'benchmark' } };
  for (const cidr of cidrs) {
    engine.insert(cidr, meta);
  }
  return engine;
}

function bench(label, n, fn, ops) {
  const WARMUP = 2;
  const RUNS   = 5;
  for (let i = 0; i < WARMUP; i++) fn();

  const times = [];
  for (let i = 0; i < RUNS; i++) {
    const t0 = performance.now();
    fn();
    times.push(performance.now() - t0);
  }
  const avg = times.reduce((a, b) => a + b, 0) / times.length;
  const throughput = ((ops / avg) * 1000).toFixed(0);
  console.log(`  ${label.padEnd(40)} ${avg.toFixed(2).padStart(8)} ms  |  ${throughput.padStart(12)} ops/s`);
}

// ---------------------------------------------------------------------------
// Run
// ---------------------------------------------------------------------------

console.log('\n=== RadixIP Node.js Benchmarks ===\n');
console.log(`  ${'Benchmark'.padEnd(40)} ${'Time'.padStart(8)}       ${'Throughput'.padStart(12)}`);
console.log('  ' + '-'.repeat(70));

const N = 50_000;

// Insert
{
  const cidrs = generateCIDRs(N);
  const meta = { value: 'bench', attributes: {} };
  bench(`insert x${N}`, N, () => {
    const e = new RadixIP();
    for (const c of cidrs) e.insert(c, meta);
  }, N);
}

// Lookup hits
{
  const e = buildEngine(N);
  const ips = generateHitIPs(N);
  bench(`lookup hit x${N}`, N, () => {
    for (const ip of ips) e.lookup(ip);
  }, N);
}

// Lookup misses
{
  const e = buildEngine(N);
  const ips = generateMissIPs(N);
  bench(`lookup miss x${N}`, N, () => {
    for (const ip of ips) e.lookup(ip);
  }, N);
}

console.log();
console.log(`  Engine size after warmup: ${buildEngine(N).size}`);
console.log('\n=== Done ===\n');
