let _gate = null;

function getGate() {
  if (_gate) return _gate;
  // require() runs at request time, not module load time
  const { radixipNext } = require('radixip/middleware');
  _gate = radixipNext({
    configPath: process.env.RADIXIP_CONFIG,
    resolveIp: (request) =>
      request.headers.get('x-forwarded-for')?.split(',')[0].trim() || null,
  });
  return _gate;
}

export function enforceRadixIP(request) {
  return getGate()(request);
}