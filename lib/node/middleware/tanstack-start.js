'use strict';

const { radixipNext } = require('./next');

/**
 * TanStack Start uses the same Web Request/Response middleware shape as
 * Next.js. Pass the returned function to the Start request middleware hook.
 */
function radixipTanStackStart(options = {}) {
  return radixipNext(options);
}

module.exports = { radixipTanStackStart };
