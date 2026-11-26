// An INTEGER autoIncrement primary key (INTEGER PRIMARY KEY AUTOINCREMENT
// on sqlite) and a references attribute (row SQ-A3).
module.exports = (sequelize, DataTypes) => {
  const point = sequelize.define(
    't_point',
    {
      id: { type: DataTypes.INTEGER, primaryKey: true, autoIncrement: true },
      area_id: { type: DataTypes.UUID, allowNull: false, references: { model: 't_area', key: 'id' } },
      label: DataTypes.STRING(20),
    },
    {},
  );

  point.associate = (models) => {
    point.belongsTo(models.Area, { foreignKey: 'area_id', targetKey: 'id', as: 'area' });
  };

  return point;
};
