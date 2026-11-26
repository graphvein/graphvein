const rewrites = [];
if (process.env.PROXY) {
  rewrites.push({ source: '/proxy/:path*', destination: process.env.PROXY });
}

module.exports = {
  basePath: process.env.BASE_PATH || '',
  rewrites: async () => rewrites,
};
