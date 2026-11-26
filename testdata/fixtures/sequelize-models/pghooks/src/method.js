const { Sequelize, DataTypes } = require('sequelize');

// Instance hooks (row D): a hook-type method on the
// instance.
const sequelize = new Sequelize({ dialect: 'postgres', logging: false });

sequelize.beforeDefine((attributes) => {
  attributes.id = { type: DataTypes.UUID, primaryKey: true };
});

sequelize.define('Method', { name: DataTypes.STRING });

module.exports = { sequelize };
