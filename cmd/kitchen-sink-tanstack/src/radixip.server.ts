import { createRequire } from 'node:module'

const require = createRequire(import.meta.url)
const { radixipTanStackStart } = require('radixip/middleware')

// The policy is created once for the server process. TanStack Start routes
// receive standards-compliant Web Requests, which the adapter supports.
const gate = radixipTanStackStart({
  configPath: process.env.RADIXIP_CONFIG,
  resolveIp: (request: Request) =>
    request.headers.get('x-forwarded-for')?.split(',')[0].trim() || null,
})

export function enforceRadixIP(request: Request): Response | undefined {
  return gate(request)
}
