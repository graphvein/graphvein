import { DataTypes, Sequelize } from 'sequelize';

// MSSQL facts are mapped (SQ-V6); an ENUM is not mapped there.
const mssql = new Sequelize({ dialect: 'mssql', logging: false });

export const Widget = mssql.define('Widget', {
  label: { type: DataTypes.STRING, allowNull: false },
  count: { type: DataTypes.INTEGER, defaultValue: 0 },
  enabled: DataTypes.BOOLEAN,
  size: DataTypes.ENUM('s', 'm'),
});

// Foreign-key actions on mssql: attributesToSQL clears
// onUpdate on the first reference to a table and both actions on a later
// reference to the same table and on a self-reference
// (dialects/mssql/query-generator.js:488-494, :542-561).
export const User = mssql.define('User', { name: DataTypes.STRING });
export const Memo = mssql.define('Memo', { title: DataTypes.STRING });
Memo.belongsTo(User, { as: 'author', onDelete: 'CASCADE', onUpdate: 'CASCADE' });
Memo.belongsTo(User, { as: 'editor', onDelete: 'CASCADE' });
Memo.belongsTo(Memo, { as: 'parent', onDelete: 'CASCADE' });
export const Org = mssql.define('Org', { name: DataTypes.STRING });
export const Group = mssql.define('Group', { name: DataTypes.STRING });
Group.belongsTo(Org);

export default mssql;
