'use strict';

// The sequelize-cli models/index.js forms, written out by
// hand instead of the template's fs.readdirSync loader: the config entry
// require(__dirname + '/../config/config.json')[env] (open point O2: the
// entries agree), the models object filled by `db.X = require(...)(...)`
// and the template's Object.keys(db).forEach loop with its if guard.
const Sequelize = require('sequelize');

const env = process.env.NODE_ENV || 'development';
const config = require(__dirname + '/../config/config.json')[env];
const sequelize = new Sequelize(config.database, config.username, config.password, config);
const db = {};

db.Author = require('./author')(sequelize, Sequelize.DataTypes);
db.Book = require('./book')(sequelize, Sequelize.DataTypes);

Object.keys(db).forEach((modelName) => {
  if (db[modelName].associate) {
    db[modelName].associate(db);
  }
});

db.sequelize = sequelize;
db.Sequelize = Sequelize;

module.exports = db;
