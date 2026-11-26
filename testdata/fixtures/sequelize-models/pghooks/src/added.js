const { Sequelize, DataTypes } = require('sequelize');

// Instance hooks (row D): addHook on the instance.
const sequelize = new Sequelize({ dialect: 'postgres', logging: false });

sequelize.addHook('beforeDefine', (attributes, options) => {
  options.tableName = 'renamed';
});

sequelize.define('Added', { name: DataTypes.STRING });

module.exports = { sequelize };
