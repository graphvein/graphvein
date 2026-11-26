const { DataTypes } = require('sequelize');

const { sequelize } = require('./db');

// References (row SQ-A3) and associations (rows SQ-R1, SQ-R2) of one
// acyclic postgres group: every model and association call is at module
// level of this module, so the execution order is the statement order.

const Owner = sequelize.define('Owner', { code: { type: DataTypes.STRING, unique: true } });

// An existing attribute that omits allowNull: belongsTo's default onDelete
// is NO ACTION; onUpdate CASCADE.
const Pet = sequelize.define('Pet', { OwnerId: DataTypes.INTEGER, name: DataTypes.STRING });

// Attributes with their own references: a define binding (init turns it
// into the table name), a string with a key and lower-case actions, a
// string without a key ("id").
const Bowl = sequelize.define(
  'Bowl',
  {
    petRef: { type: DataTypes.INTEGER, references: { model: Pet } },
    ownerRef: { type: DataTypes.INTEGER, references: { model: 'Owners', key: 'id' }, onDelete: 'set null' },
    plainRef: { type: DataTypes.INTEGER, allowNull: false, references: { model: 'Pets' }, onDelete: 'cascade', onUpdate: 'restrict' },
  },
  { underscored: true },
);

const Toy = sequelize.define('Toy', { name: DataTypes.STRING });

// A composite primary key: an association to it adds the attribute but no
// foreign key (associations/helpers.js:12-13).
const Pair = sequelize.define(
  'Pair',
  { a: { type: DataTypes.INTEGER, primaryKey: true }, b: { type: DataTypes.INTEGER, primaryKey: true } },
  { timestamps: false },
);

const Tag = sequelize.define('Tag', { label: DataTypes.STRING }, { underscored: true, createdAt: 'made' });

// The association whose existing references the merge keeps:
// ownerRef keeps its onDelete and gains onUpdate CASCADE.
Bowl.belongsTo(Owner, { foreignKey: 'ownerRef' });
Pet.belongsTo(Owner);
// belongsTo: a new nullable OwnerId (SET NULL); with as and targetKey the
// name still uses the primary key (makerId) and the type is the target
// key's.
Toy.belongsTo(Owner);
Toy.belongsTo(Owner, { as: 'maker', targetKey: 'code', onDelete: 'cascade', onUpdate: 'restrict' });
// hasOne names the key after singularize(as); hasMany with a
// foreignKey object that is not null gets CASCADE.
Owner.hasOne(Toy, { as: 'favourite' });
Owner.hasMany(Toy, { foreignKey: { name: 'keeperId', allowNull: false } });
Owner.hasMany(Toy, { foreignKey: 'looseId', constraints: false });
Toy.belongsTo(Pair);
// belongsToMany with a string through: a join table from the source's
// options (underscored, createdAt renamed) and the global timestamps.
Tag.belongsToMany(Owner, { through: 'tag_owners' });
Owner.belongsToMany(Pet, { through: 'owner_pets', foreignKey: 'ownerKey' });

module.exports = { Owner, Pet, Bowl, Toy, Pair, Tag };
