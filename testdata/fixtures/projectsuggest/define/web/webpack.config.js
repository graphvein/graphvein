// A webpack config computed in code (a function), as most are: only the
// DefinePlugin's literal argument is read.
const webpack = require('webpack');

require('dotenv').config();

const extra = { __EXTRA__: JSON.stringify('http://localhost:3100') };

module.exports = (env) => ({
  mode: env.production ? 'production' : 'development',
  plugins: [
    new webpack.DefinePlugin({
      __API_URL__: JSON.stringify(process.env.API_URL),
      __NODE_ENV__: JSON.stringify(process.env.NODE_ENV),
      __STATIC__: JSON.stringify('http://localhost:3100'),
      __QUOTED__: '"http://localhost:3999"',
      __FALLBACK__: JSON.stringify(process.env.OTHER_URL || 'http://localhost:3100'),
      __RAW__: 'process.env.API_URL',
      'process.env.SERVICE_URL': JSON.stringify(process.env.SERVICE_URL),
      DEBUG: true,
    }),
    new webpack.DefinePlugin(extra),
  ],
});
