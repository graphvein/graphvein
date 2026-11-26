// The instance module of an ESM project (row G4).
import { Sequelize, type Options } from "sequelize";
import config from "./settings";

const options: Options = { dialect: "postgres", logging: false };
const sequelize = new Sequelize(config.url, { ...options, define: { underscored: true } });
const backup = new Sequelize;

export default sequelize;
export { backup };
