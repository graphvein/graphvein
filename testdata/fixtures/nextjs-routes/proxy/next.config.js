/** @type {import('next').NextConfig} */
module.exports = {
  basePath: '/proxy',
  async rewrites() {
    return {
      beforeFiles: [
        { source: '/old/:path*', destination: '/api/new/:path*' },
        { source: '/ext/:path*', destination: 'https://ext.example.com/:path*' },
        { source: '/odd/:id', destination: '/api/v-:id' },
        { source: '/hop/:p', destination: '/api/items/:p' },
      ],
      afterFiles: [
        { source: '/api/static', destination: '/api/dest' },
        { source: '/api/items/:id', destination: '/api/things/:id' },
      ],
      fallback: [{ source: '/api/:path*', destination: '/api/fallback/:path*' }],
    };
  },
};
