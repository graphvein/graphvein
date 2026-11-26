const { Model, DataTypes } = require('sequelize');

const { sequelize } = require('../db');

// A module-level class with a static associate method.
class Genre extends Model {
  static associate(models) {
    Genre.hasMany(models.Album);
  }
}

Genre.init({ name: DataTypes.STRING }, { sequelize, modelName: 'Genre' });

module.exports = Genre;
