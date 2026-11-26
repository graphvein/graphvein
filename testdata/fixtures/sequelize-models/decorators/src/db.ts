import { Sequelize } from 'sequelize';

const sequelize = new Sequelize({ dialect: 'postgres', logging: false });

export default sequelize;
