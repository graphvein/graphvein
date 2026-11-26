// webpack-dev-server 4 object form.
module.exports = {
  devServer: {
    proxy: {
      '/v1': 'http://localhost:3100',
      '/legacy': { target: 'http://localhost:3100', pathRewrite: { '^/legacy': '' } },
    },
  },
};
