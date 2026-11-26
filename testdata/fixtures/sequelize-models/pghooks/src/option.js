const { Sequelize, DataTypes } = require('sequelize');

// Instance hooks (row D): a hooks key in the constructor
// options. beforeDefine runs inside every define and may change any
// attribute or option, so no fact of this group is claimed.
const sequelize = new Sequelize({
  dialect: 'postgres',
  logging: false,
  hooks: {
    beforeDefine(attributes) {
      attributes.extra = DataTypes.STRING;
    },
  },
});

sequelize.define('Hooked', { name: DataTypes.STRING });

module.exports = { sequelize };
