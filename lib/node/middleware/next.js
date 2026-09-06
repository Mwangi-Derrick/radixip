'use strict';

const { RadixPolicy } = require('..');
const { decisionResponse, extractIp } = require('./policy');

/**
 * Create Next.js Edge/Node middleware. Prefer a resolver that applies the
 * deployment's trusted-proxy rules instead of trusting forwarded headers here.
 */
function radixipNext(options = {}) {
  const policy = options.policy || RadixPolicy.fromYaml(options.configPath || 'config/radixip.yaml');
  const resolveIp = options.resolveIp || ((request) => request.ip || null);

  return function radixipNextMiddleware(request) {
    const ip = extractIp(request, resolveIp);
    let response;
    const next = () => {
      response = undefined;
      return undefined;
    };
    const send = (status, retryAfter, message) => {
      const headers = { 'content-type': 'application/json' };
      if (retryAfter) headers['retry-after'] = String(retryAfter);
      response = new Response(JSON.stringify({ error: message }), { status, headers });
    };

    if (!ip) {
      return new Response(JSON.stringify({ error: 'invalid client IP' }), {
        status: 400,
        headers: { 'content-type': 'application/json' },
      });
    }
    decisionResponse(policy.checkIp(ip), next, send);
    return response;
  };
}

module.exports = { radixipNext };
