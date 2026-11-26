// webpack-dev-server 5 array form.
module.exports = {
  devServer: {
    proxy: [
      { context: ['/admin-api', '/reports'], target: 'http://localhost:3100' },
      { context: (path) => path.startsWith('/x'), target: 'http://localhost:3100' },
    ],
  },
};
