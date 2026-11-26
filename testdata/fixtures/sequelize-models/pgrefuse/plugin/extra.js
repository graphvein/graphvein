const { DataTypes } = require('sequelize');

const { sequelize } = require('../src/db');

// A model of another package (pgrefuse/plugin has its own package.json)
// on the pgrefuse instance: not read, and the pgrefuse
// group is marked incomplete.
const Extra = sequelize.define('Extra', { name: DataTypes.STRING });

module.exports = { Extra };
