/** @type {import('next').NextConfig} */
const nextConfig = {
  experimental: {
    serverComponentsExternalPackages: ['radixip'],
  },
  webpack: (config, { isServer }) => {
    if (isServer) {
      const prev = config.externals ?? [];
      const prevArr = Array.isArray(prev) ? prev : [prev];
      config.externals = [
        ...prevArr,
        ({ request }, callback) => {
          if (
            request === 'radixip' ||
            request?.startsWith('radixip/') ||
            request?.startsWith('radixip-')
          ) {
            return callback(null, 'commonjs ' + request);
          }
          callback();
        },
      ];
    }
    return config;
  },
};

module.exports = nextConfig;