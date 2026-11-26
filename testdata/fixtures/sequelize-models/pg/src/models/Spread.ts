import { DataTypes, Model } from 'sequelize';

import sequelize from '../db';

const shared = { note: { type: DataTypes.TEXT } };

// A spread in the attributes object: the columns it adds and the default
// attributes are unknown.
class Spread extends Model {}

Spread.init(
  {
    ...shared,
    title: DataTypes.STRING,
  },
  { sequelize },
);

export default Spread;
