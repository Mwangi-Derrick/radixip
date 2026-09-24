'use strict';

const { existsSync } = require('fs');
const { join } = require('path');

// Map Node's platform/arch to the Rust target triple used by napi-rs
const TARGET_MAP = {
  'darwin-x64':   'x86_64-apple-darwin',
  'darwin-arm64': 'aarch64-apple-darwin',
  'win32-x64':    'x86_64-pc-windows-msvc',
  'linux-x64':    'x86_64-unknown-linux-gnu',
};

// Also need the ABI-aware suffix napi-rs actually uses in filenames
const ABI_SUFFIX = {
  'win32-x64':  'msvc',
  'linux-x64':  'gnu',
};

function getTarget() {
  const key = `${process.platform}-${process.arch}`;
  const triple = TARGET_MAP[key];
  if (!triple) {
    throw new Error(`Unsupported platform: ${key}`);
  }
  return triple;
}

function loadNative() {
  console.log(`[radixip] Detected platform: ${process.platform}-${process.arch}`);
  console.log(`[radixip] Node version: ${process.version}`);

  // 1. Local dev build
  const localPath = join(__dirname, 'radixip.node');
  console.log(`[radixip] Checking local build: ${localPath}`);
  if (existsSync(localPath)) {
    console.log(`[radixip] ✅ Loaded local build: ${localPath}`);
    return require(localPath);
  }
  console.log(`[radixip] ❌ Not found`);

  // 2. Pre-built binary for this platform
  const triple = getTarget();
  const prebuiltPath = join(__dirname, 'prebuilt', triple, 'radixip.node');
  console.log(`[radixip] Target triple: ${triple}`);
  console.log(`[radixip] Checking prebuilt: ${prebuiltPath}`);
  if (existsSync(prebuiltPath)) {
    console.log(`[radixip] ✅ Loaded prebuilt: ${prebuiltPath}`);
    return require(prebuiltPath);
  }
  console.log(`[radixip] ❌ Not found`);

  // 3a. napi-rs conventional layout WITHOUT abi suffix (macOS)
  const napiPathSimple = join(__dirname, `radixip.${process.platform}-${process.arch}.node`);
  console.log(`[radixip] Checking napi-rs layout (no abi): ${napiPathSimple}`);
  if (existsSync(napiPathSimple)) {
    console.log(`[radixip] ✅ Loaded napi-rs binary: ${napiPathSimple}`);
    return require(napiPathSimple);
  }
  console.log(`[radixip] ❌ Not found`);

  // 3b. napi-rs conventional layout WITH abi suffix (linux/win)
  const abi = ABI_SUFFIX[`${process.platform}-${process.arch}`];
  const napiPathAbi = abi
    ? join(__dirname, `radixip.${process.platform}-${process.arch}-${abi}.node`)
    : null;
  if (napiPathAbi) {
    console.log(`[radixip] Checking napi-rs layout (with abi): ${napiPathAbi}`);
    if (existsSync(napiPathAbi)) {
      console.log(`[radixip] ✅ Loaded napi-rs binary: ${napiPathAbi}`);
      return require(napiPathAbi);
    }
    console.log(`[radixip] ❌ Not found`);
  }

  throw new Error(
    `RadixIP native addon not found for ${triple}.\n` +
    `Looked in:\n` +
    `  ${localPath}\n` +
    `  ${prebuiltPath}\n` +
    `  ${napiPathSimple}\n` +
    (napiPathAbi ? `  ${napiPathAbi}\n` : '') +
    `Run \`npm run build\` or install the matching pre-built package.`
  );
}

module.exports = loadNative();