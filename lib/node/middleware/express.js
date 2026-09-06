'use strict';

const { RadixPolicy } = require('..');
const { decisionResponse, extractIp } = require('./policy');

/**
 * Create Express middleware backed by one native RadixPolicy instance.
 * Set Express trust proxy before using req.ip in deployments behind a proxy.
 */
function radixipExpress(options = {}) {
  const policy = options.policy || RadixPolicy.fromYaml(options.configPath || 'config/radixip.yaml');
  const resolveIp = options.resolveIp;

  return function radixipMiddleware(req, res, next) {
    const ip = extractIp(req, resolveIp);
    if (!ip) {
      return res.status(400).json({ error: 'invalid client IP' });
    }

    const result = policy.checkIp(ip);
    return decisionResponse(result, next, (status, retryAfter, message) => {
      if (retryAfter) res.set('Retry-After', String(retryAfter));
      return res.status(status).json({ error: message });
    });
  };
}

module.exports = { radixipExpress };
