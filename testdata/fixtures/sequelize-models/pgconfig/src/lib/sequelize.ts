import config from 'config';
import { Sequelize } from 'sequelize';

// The dialect comes from the package's single driver (pg), the options
// spread from the node-config object (no config file sets define under
// database.options: open point O1, SQ-V5), and the instance's literal
// schema applies to every model (row SQ-K1).
const sequelize = new Sequelize('fixture', 'user', 'secret', { ...config.database.options, schema: 'app', logging: false });

export default sequelize;
