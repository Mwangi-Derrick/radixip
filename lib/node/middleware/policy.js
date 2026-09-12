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
 *   2. `X-Forwarded-For` header — first (leftmost) value (the original client IP)
 *   3. `request.ip`  — set by Express with `trust proxy`, or Fastify with `trustProxy`
 *   4. `request.socket?.remoteAddress` — raw TCP address
 *
 * Reading XFF explicitly before `request.ip` ensures that the rate-limit key
 * is always the spoofed test IP, not the loopback address that the framework
 * may report when trust-proxy rules aren't perfectly configured.
 *
 * @param {object} request
 * @param {Function|undefined} resolveIp
 * @returns {string|null}
 */
function extractIp(request, resolveIp) {
  if (resolveIp) {
    return resolveIp(request) || null;
  }
  // Prefer the raw XFF header so the key is the original client, not a proxy.
  const xff = request.headers?.['x-forwarded-for'];
  if (xff) {
    return xff.split(',')[0].trim() || null;
  }
  return request.ip || request.socket?.remoteAddress || null;
}


module.exports = { decisionResponse, extractIp };
