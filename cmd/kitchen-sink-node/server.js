'use strict';

/**
 * RadixIP Node.js Kitchen Sink
 *
 * Spawns four framework servers on dedicated ports to enable E2E load tests
 * against the Node.js/N-API FFI bindings:
 *
 *   Port 8091 — Express
 *   Port 8092 — Fastify
 *
 * TanStack Start (Port 8094) and Next.js (Port 8095) are built/served by the
 * E2E orchestrator separately since they require a framework build step.
 *
 * Usage:
 *   node server.js [--config path/to/radixip.yaml]
 */

const path = require('path');

// Parse CLI arguments.
const args = process.argv.slice(2);
const configIdx = args.indexOf('--config');
const configPath = configIdx !== -1 ? args[configIdx + 1] : path.join(process.cwd(), 'config', 'radixip.yaml');

// ---------------------------------------------------------------------------
// Shared policy — created once, reused across all frameworks.
// ---------------------------------------------------------------------------
const { RadixPolicy } = require('radixip');
const { radixipExpress } = require('radixip/middleware');
const { radixipFastify } = require('radixip/middleware');

const policy = RadixPolicy.fromYaml(configPath);
console.log(`[radixip] Loaded policy from ${configPath}`);

// ---------------------------------------------------------------------------
// Express (port 8091)
// ---------------------------------------------------------------------------
const express = require('express');
const expressApp = express();

// In tests, the test runner injects X-Forwarded-For. Tell Express to trust it
// from localhost/loopback only (the test runner is local).
expressApp.set('trust proxy', ['loopback', '127.0.0.1']);
expressApp.use(radixipExpress({ policy }));

expressApp.get('/health', (_req, res) => res.json({ ok: true }));
expressApp.get('/api/v1/public', (_req, res) => res.json({ framework: 'express', route: 'public' }));
expressApp.get('/api/v1/auth', (_req, res) => res.json({ framework: 'express', route: 'auth-get' }));
expressApp.post('/api/v1/auth', (_req, res) => res.json({ framework: 'express', route: 'auth-post' }));

expressApp.listen(8091, () => console.log('🟢 Express listening on :8091'));

// ---------------------------------------------------------------------------
// Fastify (port 8092)
// ---------------------------------------------------------------------------
const Fastify = require('fastify');
const fastifyApp = Fastify({ logger: false, trustProxy: true });

// Register RadixIP as a global preHandler hook.
fastifyApp.addHook('preHandler', radixipFastify({ policy }));

fastifyApp.get('/health', async () => ({ ok: true }));
fastifyApp.get('/api/v1/public', async () => ({ framework: 'fastify', route: 'public' }));
fastifyApp.get('/api/v1/auth', async () => ({ framework: 'fastify', route: 'auth-get' }));
fastifyApp.post('/api/v1/auth', async () => ({ framework: 'fastify', route: 'auth-post' }));

fastifyApp.listen({ port: 8092, host: '0.0.0.0' }, (err) => {
  if (err) { console.error(err); process.exit(1); }
  console.log('🔵 Fastify listening on :8092');
});

// ---------------------------------------------------------------------------
// Graceful shutdown
// ---------------------------------------------------------------------------
process.on('SIGTERM', () => {
  console.log('[radixip] Received SIGTERM, shutting down...');
  fastifyApp.close(() => {
    console.log('[radixip] Fastify closed.');
    process.exit(0);
  });
});

process.on('SIGINT', () => {
  console.log('[radixip] Received SIGINT, shutting down...');
  fastifyApp.close(() => {
    process.exit(0);
  });
});
