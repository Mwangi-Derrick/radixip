'use strict';

// index.js — loads the platform-specific native addon
// napi-rs places pre-built binaries under ./binaries/<target>/radixip.node
// During `npm install`, @napi-rs/cli copies the correct binary to radixip.node

const { existsSync } = require('fs');
const { join } = require('path');

function loadNative() {
  // 1. Try local build (dev mode)
  const localPath = join(__dirname, 'radixip.node');
  if (existsSync(localPath)) {
    return require(localPath);
  }

  // 2. Try pre-built binary for the current platform/arch
  const triple = `${process.platform}-${process.arch}`;
  const prebuiltPath = join(__dirname, 'prebuilt', triple, 'radixip.node');
  if (existsSync(prebuiltPath)) {
    return require(prebuiltPath);
  }

  throw new Error(
    `RadixIP native addon not found. Run \`npm run build\` to compile from source, ` +
      `or install a pre-built package for ${triple}.`
  );
}

module.exports = loadNative();
