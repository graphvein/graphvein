/** @type {import('next').NextConfig} */
const nextConfig = {
  ...(process.env.ANALYZE ? { productionBrowserSourceMaps: true } : {}),
  trailingSlash: false,
  async rewrites() {
    return [{ source: '/api/legacy/:path*', destination: 'https://legacy.example.com/:path*' }];
  },
};

const withA = (config) => config;
const withB = (config) => config;

export default withA(withB(nextConfig));
