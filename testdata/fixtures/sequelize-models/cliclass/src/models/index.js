const { DataTypes } = require('sequelize');

const { sequelize } = require('../db');

// The models object with required classes and an inline factory call
// `require(...)(...)`, and the for...of form of the associate loop over
// Object.values with an && guard.
const models = {
  Genre: require('./genre'),
  Album: require('./album'),
  Track: require('./track')(sequelize, DataTypes),
};

for (const model of Object.values(models)) {
  model.associate && model.associate(models);
}

module.exports = models;
