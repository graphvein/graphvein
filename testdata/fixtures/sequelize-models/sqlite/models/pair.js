// A composite primary key: its members are NOT NULL on
// sqlite; no timestamps.
module.exports = (sequelize, DataTypes) => {
  const pair = sequelize.define(
    't_pair',
    {
      left_id: { type: DataTypes.INTEGER, primaryKey: true },
      right_id: { type: DataTypes.INTEGER, primaryKey: true },
      weight: { type: DataTypes.INTEGER, defaultValue: 1 },
    },
    { timestamps: false },
  );
  return pair;
};
