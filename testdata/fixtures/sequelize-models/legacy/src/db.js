const Sequelize = require('sequelize');

// sequelize 5 (the lockfile pins 5.22.5): another major, refused.
const sequelize = new Sequelize({ dialect: 'postgres' });

const Old = sequelize.define('Old', { name: Sequelize.STRING });

module.exports = { sequelize, Old };
