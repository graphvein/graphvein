// Outside the gate (row GD): no Sequelize import or require, no init or
// define form, no Sequelize dialect, no environment selector.
// require("sequelize") in a comment does not count.
const sequelizeTypescript = require("sequelize-typescript");
const config = require("config");

customElements.define("x-panel", Panel);
Store.init(settings, { strict: true });
if (ready) {
  Store.init({}, {});
}

module.exports = {
  drizzle: { dialect: "postgresql" },
  pick: () => config[process.env.NODE_ENV],
};
