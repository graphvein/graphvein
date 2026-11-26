// A CommonJS index: the instance, factories and the associate loop (row G5).
"use strict";
const Sequelize = require("sequelize");
const { DataTypes, Op: Operators, ...rest } = require("sequelize");
const [first] = list;
const getConfig = require("../utils/getConfig");
const AreaModel = require("./area");

const config = getConfig();
const sequelize = new Sequelize("mainDB", null, null, config);

const models = {
  Area: AreaModel(sequelize, Sequelize),
};

Object.values(models)
  .filter((model) => typeof model.associate === "function")
  .forEach((model) => model.associate(models));

for (const name of Object.keys(models)) {
  models[name].ready = true;
}

module.exports = { ...models, sequelize, Operators };
