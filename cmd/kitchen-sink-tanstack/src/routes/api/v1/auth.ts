import { createFileRoute } from '@tanstack/react-router'
import { enforceRadixIP } from '~/radixip.server'

export const Route = createFileRoute('/api/v1/auth')({
  server: {
    handlers: {
      POST: ({ request }: { request: Request }) =>
        enforceRadixIP(request) || Response.json({ framework: 'tanstack-start', route: 'auth-post' }),
    },
  },
})
