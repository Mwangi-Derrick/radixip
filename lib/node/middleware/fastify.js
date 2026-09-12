'use strict';

const { RadixPolicy } = require('..');
const { extractIp } = require('./policy');

/**
 * Create a Fastify preHandler hook backed by one native RadixPolicy instance.
 * Register via fastify.addHook('preHandler', radixipFastify({ ... })).
 */
function radixipFastify(options = {}) {
  const policy = options.policy || RadixPolicy.fromYaml(options.configPath || 'config/radixip.yaml');
  const resolveIp = options.resolveIp;

  return async function radixipPreHandler(request, reply) {
    const ip = extractIp(request, resolveIp);
    if (!ip) {
      return reply.status(400).send({ error: 'invalid client IP' });
    }

    // `request.url` can include a query string; route matching uses paths.
    const result = policy.checkRequest(ip, request.method, request.url.split('?', 1)[0]);
    if (result.decision === 'allow') {
      // Fastify preHandlers continue by returning nothing.
      return;
    }
    if (result.decision === 'limit') {
      reply.header('Retry-After', String(result.retryAfterSeconds || 1));
      return reply.status(429).send({ error: 'rate limited' });
    }
    // block / auto_ban both map to 403
    return reply.status(403).send({ error: 'blocked' });
  };
}

/**
 * Fastify plugin style — registers a global preHandler hook and decorates
 * the instance with `fastify.radixip.check(ip)` for manual use.
 *
 * @example
 * fastify.register(radixipFastifyPlugin, { configPath: 'config/radixip.yaml' });
 */
async function radixipFastifyPlugin(fastify, options = {}) {
  // Create ONE shared policy instance for the entire plugin lifecycle.
  const policy = options.policy || RadixPolicy.fromYaml(options.configPath || 'config/radixip.yaml');

  fastify.decorate('radixip', {
    check: (ip) => policy.checkIp(ip),
  });

  // Add preHandler hook for global protection (opt-out with global: false).
  if (options.global !== false) {
    const hook = radixipFastify({ policy, resolveIp: options.resolveIp });
    fastify.addHook('preHandler', hook);
  }
}

module.exports = {
  radixipFastify,
  radixipFastifyPlugin,
};
