import { DataTypes, Model } from 'sequelize';

import sequelize from '../db';

// A mutation after init: the implicit id is removed.
class Settlement extends Model {}

Settlement.init(
  {
    group: { type: DataTypes.UUID, allowNull: false },
  },
  { sequelize, tableName: 'settlements', timestamps: false },
);

Settlement.removeAttribute('id');

export default Settlement;
