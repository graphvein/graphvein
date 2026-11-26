'use strict';

const { Model } = require('sequelize');

// The sequelize-cli model template: a class and its init inside the
// factory. It is not read (a marker, the group's table list
// may be incomplete); models/index.js does not load it.
module.exports = (sequelize, DataTypes) => {
  class Legacy extends Model {
    static associate(models) {
      // define association here
    }
  }
  Legacy.init(
    {
      code: DataTypes.STRING,
    },
    {
      sequelize,
      modelName: 'Legacy',
    },
  );
  return Legacy;
};
