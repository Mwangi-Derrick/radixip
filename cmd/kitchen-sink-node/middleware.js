import { radixipNext } from 'radixip/middleware';

// Note: RadixIP native addon requires the Node.js runtime, so we must
// enforce Node.js for this middleware if running in Next.js.
// However, standard Next.js edge middleware does not support native addons.
// The orchestrator builds this app, and we can only use it in a Node runtime.
export const config = {
  matcher: ['/api/:path*'],
};

export default radixipNext({
  configPath: '../../config/radixip.yaml'
});
