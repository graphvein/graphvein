// No dotenv: process.env holds only the shell's environment here, so no
// env file is tied to __PROD_URL__.
const { DefinePlugin } = require('webpack');

module.exports = {
  plugins: [new DefinePlugin({ __PROD_URL__: JSON.stringify(process.env.PROD_URL) })],
};
