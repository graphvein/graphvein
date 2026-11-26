const { Model, DataTypes } = require('sequelize');

const { sequelize } = require('../db');

class Album extends Model {
  static associate(models) {
    Album.belongsTo(models.Genre, { as: 'primary', onDelete: 'CASCADE', onUpdate: 'RESTRICT' });
  }
}

Album.init({ title: { type: DataTypes.STRING, allowNull: false } }, { sequelize, modelName: 'Album' });

module.exports = Album;
