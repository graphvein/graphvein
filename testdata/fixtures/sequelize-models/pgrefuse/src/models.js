const { DataTypes } = require('sequelize');

const { sequelize } = require('./db');

// Association and references forms the rows refuse (rows SQ-A3, SQ-R1,
// SQ-R2): the facts they may add are unknown.
const clubTable = 'Clubs';

const Member = sequelize.define('Member', { role: DataTypes.STRING });
const Club = sequelize.define('Club', { name: DataTypes.STRING });
// A references model that is not a string literal or a model.
const Person = sequelize.define('Person', { name: DataTypes.STRING, clubRef: { type: DataTypes.INTEGER, references: { model: clubTable } } });

// Options that are not an object literal: any attribute key of the holder
// may be merged, so every Person column is unknown.
const personOptions = { as: 'club' };
Person.belongsTo(Club, personOptions);
// constraints: false with an onDelete still writes references (not a form
// the oracle compared): the holder's foreign keys are unknown.
Club.hasMany(Member, { foreignKey: 'clubId', constraints: false, onDelete: 'CASCADE' });
// A self-association over a string through: the join table is not
// claimed (marker).
Club.belongsToMany(Club, { through: 'club_links', as: 'partners' });
// A through model: the keys it assigns and the implicit id it deletes are
// unknown.
Club.belongsToMany(Person, { through: Member });

module.exports = { Member, Club, Person };
