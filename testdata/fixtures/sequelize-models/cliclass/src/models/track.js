module.exports = (sequelize, DataTypes) => {
  const Track = sequelize.define('Track', { title: DataTypes.STRING });
  Track.associate = (models) => {
    Track.belongsTo(models.Album);
  };
  return Track;
};
