'use strict';

const { RadixPolicy } = require('..');
const { decisionResponse, extractIp } = require('./policy');

/**
 * Create Fastify middleware backed by one native RadixPolicy instance.
 * Set Fastify trust proxy before using req.ip in deployments behind a proxy.
 */
function radixipFastify(options = {}) {
  const policy = options.policy || RadixPolicy.fromYaml(options.configPath || 'config/radixip.yaml');
  const resolveIp = options.resolveIp;

  return async function radixipMiddleware(request, reply) {
    const ip = extractIp(request, resolveIp);
    if (!ip) {
      return reply.status(400).send({ error: 'invalid client IP' });
    }

    const result = policy.checkIp(ip);
    
    // Fastify version of decisionResponse
    const response = await decisionResponse(result, null, (status, retryAfter, message) => {
      if (retryAfter) reply.header('Retry-After', String(retryAfter));
      return reply.status(status).send({ error: message });
    });
    
    return response;
  };
}

// Alternative: Fastify plugin style
async function radixipFastifyPlugin(fastify, options = {}) {
  const middleware = radixipFastify(options);
  
  fastify.decorate('radixip', {
    check: (ip) => {
      const policy = options.policy || RadixPolicy.fromYaml(options.configPath || 'config/radixip.yaml');
      return policy.checkIp(ip);
    },
    middleware: middleware
  });

  // Add preHandler hook if you want global protection
  if (options.global !== false) {
    fastify.addHook('preHandler', middleware);
  }
}

module.exports = { 
  radixipFastify,
  radixipFastifyPlugin 
};