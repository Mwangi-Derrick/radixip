'use strict';

const { RadixPolicy } = require('..');
const { extractIp } = require('./policy');

/**
 * Create a Next.js / Web-API compatible middleware function.
 *
 * The returned function accepts a standard `Request` and returns a `Response`
 * or `undefined`. Use it in `middleware.ts` at the root of your Next.js project
 * or as a TanStack Start request middleware.
 *
 * IMPORTANT: The native addon requires the Node.js runtime. It cannot run in
 * the Next.js Edge Runtime. Set `export const runtime = 'nodejs'` or deploy
 * the middleware on a Node-capable host.
 */
function radixipNext(options = {}) {
  const policy = options.policy || RadixPolicy.fromYaml(options.configPath || 'config/radixip.yaml');
  const resolveIp = options.resolveIp || ((request) => {
    // Try standard forwarded header, then fall back to nothing.
    // Callers behind a proxy should supply a trusted resolveIp function.
    return request.headers?.get?.('x-real-ip') || null;
  });

  return function radixipNextMiddleware(request, _event) {
    const ip = extractIp(request, resolveIp);
    if (!ip) {
      return new Response(JSON.stringify({ error: 'invalid client IP' }), {
        status: 400,
        headers: { 'content-type': 'application/json' },
      });
    }

    // Pass the concrete target so configured route-trie limits are honoured.
    // NextRequest exposes `nextUrl`, while TanStack and standard Web Requests
    // expose `url` as a string. Support both adapter targets.
    const path = request.nextUrl?.pathname || new URL(request.url).pathname;
    const result = policy.checkRequest(ip, request.method, path);

    if (result.decision === 'allow') {
      // Return undefined to signal the middleware chain should continue.
      // In Next.js, returning undefined from middleware passes the request through.
      return undefined;
    }

    if (result.decision === 'limit') {
      return new Response(JSON.stringify({ error: 'rate limited' }), {
        status: 429,
        headers: {
          'content-type': 'application/json',
          'retry-after': String(result.retryAfterSeconds || 1),
        },
      });
    }

    // block / auto_ban
    return new Response(JSON.stringify({ error: 'blocked' }), {
      status: 403,
      headers: { 'content-type': 'application/json' },
    });
  };
}

module.exports = { radixipNext };
