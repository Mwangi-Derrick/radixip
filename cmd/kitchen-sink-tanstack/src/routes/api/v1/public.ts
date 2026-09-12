import { createFileRoute } from '@tanstack/react-router'
import { enforceRadixIP } from '~/radixip.server'

export const Route = createFileRoute('/api/v1/public')({
  server: {
    handlers: {
      GET: ({ request }: { request: Request }) =>
        enforceRadixIP(request) || Response.json({ framework: 'tanstack-start', route: 'public' }),
    },
  },
})
