const { DataTypes } = require('sequelize');

const { sequelize } = require('./db');

// freezeTableName keeps the model name even with
// underscored; column names are still underscored.
const Reaction = sequelize.define('Reaction', { emojiCode: DataTypes.STRING(8) }, { freezeTableName: true });

// A schema option on postgres (row SQ-K2).
const Report = sequelize.define('Report', { body: DataTypes.TEXT }, { schema: 'reports', paranoid: false });

// A BIGINT autoIncrement primary key (BIGSERIAL); Tally -> tallies.
const Tally = sequelize.define('Tally', { id: { type: DataTypes.BIGINT, primaryKey: true, autoIncrement: true } }, { timestamps: false });

// Default forms (row SQ-A2): any literal on JSON and JSONB
// is JSON.stringify text.
const Setting = sequelize.define('Setting', {
  label: { type: DataTypes.STRING, defaultValue: 'untitled' },
  body: { type: DataTypes.TEXT, defaultValue: 'empty' },
  tiny: { type: DataTypes.SMALLINT, defaultValue: 7 },
  word: { type: DataTypes.JSON, defaultValue: 'x' },
  count: { type: DataTypes.JSON, defaultValue: 3 },
  weight: { type: DataTypes.JSONB, defaultValue: 4 },
  on: { type: DataTypes.JSON, defaultValue: true },
  off: { type: DataTypes.JSONB, defaultValue: false },
  shape: { type: DataTypes.JSONB, defaultValue: { a: 1 } },
});

module.exports = { Reaction, Report, Tally, Setting };
