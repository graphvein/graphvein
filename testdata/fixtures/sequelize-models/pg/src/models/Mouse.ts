import { DataTypes, Model } from 'sequelize';

import sequelize from '../db';

// Options: paranoid, version, renamed and disabled timestamps, indexes
// (generated and literal names, unique, using, a partial index, field
// objects) and a JSONB index. Mouse pluralizes to Mice.
class Mouse extends Model {}

Mouse.init(
  {
    name: DataTypes.STRING,
    region: DataTypes.STRING,
    data: { type: DataTypes.JSONB, index: true },
    rank: DataTypes.INTEGER,
    serial: DataTypes.STRING,
  },
  {
    sequelize,
    paranoid: true,
    version: true,
    createdAt: 'bornAt',
    updatedAt: false,
    indexes: [
      { fields: ['name', 'region'] },
      { fields: ['serial'], unique: true },
      { name: 'mice_rank_desc', fields: [{ name: 'rank', order: 'DESC' }], using: 'BTREE' },
      { fields: ['region'], where: { rank: 1 } },
    ],
  },
);

export default Mouse;
