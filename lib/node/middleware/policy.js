'use strict';

function decisionResponse(result, next, send) {
  if (result.decision === 'allow') {
    return next();
  }

  if (result.decision === 'limit') {
    return send(429, result.retryAfterSeconds || 1, 'rate limited');
  }

  if (result.decision === 'block') {
    return send(403, 0, 'blocked');
  }

  return send(400, 0, 'invalid client IP');
}

function extractIp(request, resolveIp) {
  if (resolveIp) {
    return resolveIp(request);
  }
  return request.ip || request.socket?.remoteAddress || null;
}

module.exports = { decisionResponse, extractIp };
