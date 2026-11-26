const { DataTypes } = require('sequelize');

const { sequelize } = require('./db');

// A cyclic foreign-key graph: Users and Teams reference each
// other, so sync creates every table without foreign keys and adds them
// in a per-model alter pass, which skips primary-key columns and stops at
// the first attribute whose changeColumn SQL postgres rejects.
const Team = sequelize.define('Team', { name: DataTypes.STRING });
const User = sequelize.define('User', { name: DataTypes.STRING });

// A foreign key on a primary-key column: never added (unknown).
const Profile = sequelize.define('Profile', {
  UserId: { type: DataTypes.INTEGER, primaryKey: true, references: { model: 'Users', key: 'id' } },
  bio: DataTypes.TEXT,
});

// A default followed by UNIQUE: its changeColumn SQL is invalid, so none
// of the model's foreign keys is claimed.
const Badge = sequelize.define('Badge', { code: { type: DataTypes.STRING, defaultValue: 'x', unique: true } });

// A model whose alter pass succeeds: its foreign key is claimed.
const Note = sequelize.define('Note', { body: DataTypes.TEXT, title: { type: DataTypes.STRING, field: 'note_title' } });

User.belongsTo(Team);
Team.belongsTo(User, { as: 'owner' });
Badge.belongsTo(Team);
Note.belongsTo(User);

module.exports = { Team, User, Profile, Badge, Note };
