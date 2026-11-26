import { DataTypes, Model } from 'sequelize';

import sequelize from '../lib/sequelize';

class Account extends Model {}

Account.init({ email: { type: DataTypes.STRING, allowNull: false } }, { sequelize });

export default Account;
