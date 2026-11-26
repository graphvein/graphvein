'use strict';

module.exports = (sequelize, DataTypes) => {
  const Book = sequelize.define('Book', {
    title: DataTypes.STRING,
  });
  Book.associate = function (models) {
    Book.belongsTo(models.Author, { as: 'editor', onDelete: 'CASCADE' });
  };
  return Book;
};
