// Cases for TestDDLRendering: each define is rendered by Sequelize
// 6.37.8's own query generator (attributesToSQL and createTableQuery) on
// each dialect, and the claim pass's facts for the same source must agree
// with that text. __DIALECT__ is replaced by the dialect.
const { Sequelize, DataTypes } = require('sequelize');

const sequelize = new Sequelize({ dialect: '__DIALECT__', logging: false });

sequelize.define('Kind', {
  s: DataTypes.STRING,
  s40: DataTypes.STRING(40),
  c: DataTypes.CHAR(4),
  t: DataTypes.TEXT,
  i: DataTypes.INTEGER,
  bi: DataTypes.BIGINT,
  si: DataTypes.SMALLINT,
  f: DataTypes.FLOAT,
  d: DataTypes.DOUBLE,
  dec: DataTypes.DECIMAL,
  dec2: DataTypes.DECIMAL(10, 2),
  b: DataTypes.BOOLEAN,
  dt: DataTypes.DATE,
  don: DataTypes.DATEONLY,
  tm: DataTypes.TIME,
  u: DataTypes.UUID,
  bl: DataTypes.BLOB,
  e: DataTypes.ENUM('a', 'b'),
  ev: { type: DataTypes.ENUM, values: ['x', 'y'] },
});

sequelize.define('Json', { j: { type: DataTypes.JSON, defaultValue: { a: [1, 'x'] } }, js: { type: DataTypes.JSON, defaultValue: '{}' } });

sequelize.define('Defaults', {
  s: { type: DataTypes.STRING, defaultValue: "it's" },
  c: { type: DataTypes.CHAR(2), defaultValue: 'ab' },
  t: { type: DataTypes.TEXT, defaultValue: 'tt' },
  i: { type: DataTypes.INTEGER, defaultValue: 3, allowNull: false },
  n: { type: DataTypes.INTEGER, defaultValue: -3 },
  bi: { type: DataTypes.BIGINT, defaultValue: 10 },
  f: { type: DataTypes.FLOAT, defaultValue: 1.5 },
  d: { type: DataTypes.DOUBLE, defaultValue: 2 },
  dec: { type: DataTypes.DECIMAL(10, 2), defaultValue: 1.25 },
  b: { type: DataTypes.BOOLEAN, defaultValue: true },
  bf: { type: DataTypes.BOOLEAN, defaultValue: false, allowNull: false },
  e: { type: DataTypes.ENUM('x', 'y'), defaultValue: 'y' },
  nul: { type: DataTypes.STRING, defaultValue: null },
  now: { type: DataTypes.DATE, defaultValue: DataTypes.NOW },
  uid: { type: DataTypes.UUID, defaultValue: DataTypes.UUIDV4 },
  lit: { type: DataTypes.STRING, defaultValue: sequelize.literal("'lit'") },
});

sequelize.define('Uniques', {
  a: { type: DataTypes.STRING, unique: true },
  g1: { type: DataTypes.STRING, unique: 'pair' },
  g2: { type: DataTypes.STRING, unique: 'pair' },
  n: { type: DataTypes.STRING, unique: { name: 'solo' } },
  off: { type: DataTypes.STRING, unique: false },
});

sequelize.define('Keys', {
  code: { type: DataTypes.STRING, primaryKey: true },
  other: { type: DataTypes.INTEGER, allowNull: false },
}, { timestamps: false });

sequelize.define('UuidKey', { id: { type: DataTypes.UUID, primaryKey: true }, v: DataTypes.STRING }, { timestamps: false });

sequelize.define('BigKey', { id: { type: DataTypes.BIGINT, primaryKey: true, autoIncrement: true }, v: DataTypes.STRING }, { timestamps: false });

sequelize.define('Composite', {
  l: { type: DataTypes.INTEGER, primaryKey: true },
  r: { type: DataTypes.INTEGER, primaryKey: true, allowNull: false },
  w: DataTypes.INTEGER,
}, { timestamps: false });

sequelize.define('Stamps', { v: DataTypes.STRING }, { paranoid: true, version: true, createdAt: 'made', updatedAt: false });

sequelize.define('Snake', { readCount: DataTypes.INTEGER, title: { type: DataTypes.STRING, field: 'heading' } }, { underscored: true });

// Foreign keys: mssql's attributesToSQL clears onUpdate on
// the first reference to a table and both actions on a later one and on a
// self-reference; the other dialects write the actions as given.
const User = sequelize.define('User', { name: DataTypes.STRING });
const Memo = sequelize.define('Memo', { title: DataTypes.STRING });
Memo.belongsTo(User, { as: 'author', onDelete: 'CASCADE', onUpdate: 'CASCADE' });
Memo.belongsTo(User, { as: 'editor', onDelete: 'CASCADE' });
Memo.belongsTo(Memo, { as: 'parent', onDelete: 'CASCADE' });
const Org = sequelize.define('Org', { name: DataTypes.STRING });
const Team = sequelize.define('Team', { name: DataTypes.STRING });
Team.belongsTo(Org);
sequelize.define('Pin', {
  a: { type: DataTypes.INTEGER, references: { model: 'Orgs', key: 'id' }, onDelete: 'SET NULL', onUpdate: 'CASCADE' },
  b: { type: DataTypes.INTEGER, references: { model: 'Orgs' }, onDelete: 'CASCADE', onUpdate: 'CASCADE' },
  c: { type: DataTypes.INTEGER, references: { model: 'Users' }, onUpdate: 'RESTRICT' },
});
