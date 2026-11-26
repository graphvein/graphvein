const { DataTypes } = require('sequelize');

const { sequelize } = require('./db');

// Loaded after models.js (src/index.js), whose load throws: never runs.
// The claim pass cannot prove the order between modules, so Late is not
// claimed.
const Late = sequelize.define('Late', { name: DataTypes.STRING });

module.exports = { Late };
