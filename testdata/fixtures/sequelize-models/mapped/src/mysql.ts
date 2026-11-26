import { DataTypes, Sequelize } from 'sequelize';

// MySQL facts are mapped from Sequelize's query generator (SQ-V6).
const mysql = new Sequelize({ dialect: 'mysql', logging: false });

export const Gadget = mysql.define('Gadget', {
  serial: { type: DataTypes.STRING(30), allowNull: false, unique: true },
  enabled: { type: DataTypes.BOOLEAN, defaultValue: true },
  token: DataTypes.UUID,
  notes: { type: DataTypes.TEXT, defaultValue: 'none' },
  size: DataTypes.ENUM('s', 'm'),
});

export default mysql;
