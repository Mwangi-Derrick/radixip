import { radixipNext } from 'radixip/middleware';

// Next.js root middleware is Edge-only and cannot load a native addon. Route
// handlers run in the Node.js runtime, so this uses the same Web Request
// adapter at the supported boundary instead.
const gate = radixipNext({
  configPath: process.env.RADIXIP_CONFIG,
  resolveIp: (request) => request.headers.get('x-forwarded-for')?.split(',')[0].trim() || null,
});

export function enforceRadixIP(request) {
  return gate(request);
}
