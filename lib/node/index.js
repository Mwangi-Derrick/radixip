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

function getTarget() {
  const key = `${process.platform}-${process.arch}`;
  const triple = TARGET_MAP[key];
  if (!triple) {
    throw new Error(`Unsupported platform: ${key}`);
  }
  return triple;
}

function loadNative() {
  // 1. Local dev build (created by `napi build --release --platform`)
  const localPath = join(__dirname, 'radixip.node');
  if (existsSync(localPath)) {
    return require(localPath);
  }

  // 2. Pre-built binary for this platform, e.g. prebuilt/x86_64-apple-darwin/radixip.node
  const triple = getTarget();
  const prebuiltPath = join(__dirname, 'prebuilt', triple, 'radixip.node');
  if (existsSync(prebuiltPath)) {
    return require(prebuiltPath);
  }

  // 3. napi-rs conventional layout (optional fallback)
  const napiPath = join(__dirname, `radixip.${process.platform}-${process.arch}.node`);
  if (existsSync(napiPath)) {
    return require(napiPath);
  }

  throw new Error(
    `RadixIP native addon not found for ${triple}.\n` +
    `Looked in:\n  ${localPath}\n  ${prebuiltPath}\n  ${napiPath}\n` +
    `Run \`npm run build\` or install the matching pre-built package.`
  );
}

module.exports = loadNative();