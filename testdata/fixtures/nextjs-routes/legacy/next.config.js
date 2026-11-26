/** @type {import('next').NextConfig} */
module.exports = {
  // Incremental adoption (T024): an afterFiles rule with an
  // external destination whose source is a static route, and a fallback
  // to the old site for every path no route serves.
  async rewrites() {
    return {
      afterFiles: [{ source: '/api/head', destination: 'https://legacy.example.com/api/head' }],
      fallback: [{ source: '/:path*', destination: 'https://legacy.example.com/:path*' }],
    };
  },
};
