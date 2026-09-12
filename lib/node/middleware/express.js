'use strict';

const { RadixPolicy } = require('..');
const { decisionResponse, extractIp } = require('./policy');

/**
 * Create Express middleware backed by one native RadixPolicy instance.
 *
 * Call `app.set('trust proxy', ...)` before attaching this middleware so that
 * Express resolves `req.ip` from the correct forwarded header. For fine-grained
 * control, pass a `resolveIp(req)` function instead.
 *
 * @example
 * const app = express();
 * app.set('trust proxy', ['loopback', '10.0.0.0/8']);
 * app.use(radixipExpress({ configPath: 'config/radixip.yaml' }));
 *
 * @param {object} options
 * @param {string}        [options.configPath]  - Path to radixip.yaml (default 'config/radixip.yaml')
 * @param {object}        [options.policy]      - Pre-created RadixPolicy (overrides configPath)
 * @param {Function}      [options.resolveIp]   - Custom IP extractor `(req) => string|null`
 */
function radixipExpress(options = {}) {
  const policy = options.policy || RadixPolicy.fromYaml(options.configPath || 'config/radixip.yaml');
  const resolveIp = options.resolveIp;

  return function radixipMiddleware(req, res, next) {
    const ip = extractIp(req, resolveIp);
    if (!ip) {
      return res.status(400).json({ error: 'invalid client IP' });
    }

    // Pass the concrete target so configured route-trie limits are honoured.
    const result = policy.checkRequest(ip, req.method, req.path);
    return decisionResponse(result, next, (status, retryAfter, message) => {
      if (retryAfter) res.set('Retry-After', String(retryAfter));
      return res.status(status).json({ error: message });
    });
  };
}

module.exports = { radixipExpress };
