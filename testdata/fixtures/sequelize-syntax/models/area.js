// A CommonJS factory that imports nothing from Sequelize (row G6, define).
const { addSelector } = require("../utils/selector");

module.exports = (sequelize, DataTypes) => {
  const area = sequelize.define(
    "t_area",
    {
      id: { type: DataTypes.UUID, primaryKey: true, defaultValue: DataTypes.UUIDV4 },
      name: { type: DataTypes.STRING, allowNull: !false, unique: true },
      radius: { type: DataTypes.DOUBLE, defaultValue: -1.5 },
    },
    {},
  );
  const { hooks = {}, nested: { inner } } = area.options;
  let pending;

  area.associate = (models) => {
    area.belongsTo(models.House, { foreignKey: "house_id", targetKey: "id", as: "house" });
    area.hasMany(models.Room, { foreignKey: "area_id", sourceKey: "id" });
  };

  class Helper extends Object {}

  if (hooks.enabled) {
    area.beforeValidate(addSelector);
  }
  function local() {}
  return area;
};
