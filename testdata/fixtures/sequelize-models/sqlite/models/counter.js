// A BIGINT primary key becomes INTEGER PRIMARY KEY on
// sqlite.
module.exports = (sequelize, DataTypes) => {
  const counter = sequelize.define(
    't_counter',
    {
      id: { type: DataTypes.BIGINT, primaryKey: true },
      hits: { type: DataTypes.BIGINT, allowNull: false, defaultValue: 0 },
    },
    { createdAt: false },
  );
  return counter;
};
