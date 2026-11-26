import { DataTypes, Model } from 'sequelize';

import sequelize from '../lib/sequelize';
import Account from './Account';

// references with a model class (init replaces it by the class's table,
// here in the instance's schema app) and with a table name, which
// postgres resolves in the referencing table's schema.
class Session extends Model {}

Session.init(
  {
    accountRef: { type: DataTypes.INTEGER, references: { model: Account, key: 'id' }, onDelete: 'CASCADE' },
    otherRef: { type: DataTypes.INTEGER, references: { model: 'Accounts' } },
  },
  { sequelize },
);

export default Session;
