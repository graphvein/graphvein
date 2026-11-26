const Sequelize = require('sequelize');

const AreaModel = require('./area');
const PointModel = require('./point');
const PairModel = require('./pair');
const CounterModel = require('./counter');

// CommonJS factories called with the instance, and the recognised
// associate(models) loop (row SQ-K1); the global define is Gladys's.
const config = {
  dialect: 'sqlite',
  storage: ':memory:',
  logging: false,
  define: { underscored: true, freezeTableName: true, createdAt: 'created_at', updatedAt: 'updated_at' },
};

const sequelize = new Sequelize(config);

const models = {
  Area: AreaModel(sequelize, Sequelize),
  Point: PointModel(sequelize, Sequelize),
  Pair: PairModel(sequelize, Sequelize),
  Counter: CounterModel(sequelize, Sequelize),
};

Object.values(models)
  .filter((model) => typeof model.associate === 'function')
  .forEach((model) => model.associate(models));

module.exports = { sequelize, models };
