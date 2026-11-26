import { Sequelize } from 'sequelize';

// The instance of the pg package: a literal dialect, no global define.
const sequelize = new Sequelize({ dialect: 'postgres', host: 'localhost', database: 'fixture', logging: false });

export default sequelize;
