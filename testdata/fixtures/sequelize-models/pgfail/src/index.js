// The entry of the pgfail package: models.js throws while it loads, so
// late.js is never required.
const models = require('./models');
const late = require('./late');

module.exports = { ...models, ...late };
