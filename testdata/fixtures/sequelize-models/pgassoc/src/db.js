const { Sequelize } = require('sequelize');

// The instance of the pgassoc package: postgres, no global define.
const sequelize = new Sequelize({ dialect: 'postgres', database: 'fixture', logging: false });

module.exports = { sequelize };
