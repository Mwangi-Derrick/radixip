'use strict';

/**
 * Shared helper: map a RadixPolicy result to a framework response.
 *
 * @param {object} result   - Result from `policy.checkIp(ip)`
 * @param {Function} next   - Framework next() function (called on allow)
 * @param {Function} send   - `(status, retryAfterSeconds, message) => response`
 */
function decisionResponse(result, next, send) {
  switch (result.decision) {
    case 'allow':
      return next();
    case 'limit':
      return send(429, result.retryAfterSeconds || 1, 'rate limited');
    case 'block':
    case 'auto_ban':
      return send(403, 0, 'blocked');
    case 'bad_request':
    default:
      return send(400, 0, 'invalid client IP');
  }
}

/**
 * Extract the client IP from a framework request.
 *
 * Priority:
 *   1. Custom `resolveIp(request)` callback (lets callers apply trusted-proxy rules)
 *   2. `request.ip`  — set by Express with `trust proxy`, or Fastify
 *   3. `request.socket?.remoteAddress` — raw TCP address
 *
 * For Next.js and TanStack Start, always supply a `resolveIp` function because
 * those environments do not expose `request.ip` in the same way.
 *
 * @param {object} request
 * @param {Function|undefined} resolveIp
 * @returns {string|null}
 */
function extractIp(request, resolveIp) {
  if (resolveIp) {
    return resolveIp(request) || null;
  }
  return request.ip || request.socket?.remoteAddress || null;
}

module.exports = { decisionResponse, extractIp };
