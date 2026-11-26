const { DataTypes } = require('sequelize');

const { sequelize } = require('./db');

// Provable failures of references and associations (row SQ-P1): the
// models involved are not claimed.
const Home = sequelize.define('Home', { name: DataTypes.STRING });

// A belongsTo from a model in another schema writes REFERENCES
// "other"."Homes", a table sync does not create.
const Away = sequelize.define('Away', { name: DataTypes.STRING }, { schema: 'other' });

// references naming a column the referenced table does not have.
const Broken = sequelize.define('Broken', { homeRef: { type: DataTypes.INTEGER, references: { model: 'Homes', key: 'nope' } } });

const Lost = sequelize.define('Lost', { name: DataTypes.STRING });

// A belongsTo targetKey that is neither the primary key nor
// unique: postgres rejects the foreign key, so Racks is not
// created, and Bins, whose references name Racks, is not either.
const Shelf = sequelize.define('Shelf', { code: DataTypes.STRING });
const Rack = sequelize.define('Rack', { name: DataTypes.STRING });
const Bin = sequelize.define('Bin', { rackRef: { type: DataTypes.INTEGER, references: { model: 'Racks' } } });
Rack.belongsTo(Shelf, { as: 'byCode', targetKey: 'code' });

Away.belongsTo(Home);
// belongsTo throws for a targetKey the target does not have
// (associations/belongs-to.js:51-53): loading stops here.
Lost.belongsTo(Home, { targetKey: 'missing' });

// Never runs: defined after the throw, so not claimed.
const After = sequelize.define('After', { name: DataTypes.STRING });

module.exports = { Home, Away, Broken, Lost, Shelf, Rack, Bin, After };
