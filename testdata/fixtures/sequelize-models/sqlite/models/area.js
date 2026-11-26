// sqlite forms (investigation probe 1 on SQLite, Gladys): a UUID primary key is
// nullable, booleans default to 1/0, a DOUBLE number default
// is quoted, ENUM is TEXT, a JSON default is JSON.stringify text.
module.exports = (sequelize, DataTypes) => {
  const area = sequelize.define(
    't_area',
    {
      id: { type: DataTypes.UUID, primaryKey: true, defaultValue: DataTypes.UUIDV4 },
      name: { type: DataTypes.STRING, allowNull: false, unique: true },
      visible: { type: DataTypes.BOOLEAN, allowNull: false, defaultValue: false },
      zoom: { type: DataTypes.DOUBLE, defaultValue: 1.5 },
      kind: { type: DataTypes.ENUM('room', 'zone'), defaultValue: 'room' },
      settings: { type: DataTypes.JSON, defaultValue: {} },
      since: DataTypes.DATEONLY,
      touched: { type: DataTypes.DATE, defaultValue: sequelize.literal('CURRENT_TIMESTAMP') },
    },
    {},
  );

  area.associate = (models) => {
    area.hasMany(models.Point, { foreignKey: 'area_id', sourceKey: 'id', as: 'points' });
    // A join table on sqlite (row SQ-R2): the keys' types are the primary
    // keys' (UUID, BIGINT), together the primary key, NOT NULL.
    area.belongsToMany(models.Counter, { through: 't_area_counters' });
  };

  return area;
};
