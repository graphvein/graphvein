import { DataTypes, Model } from 'sequelize';

import sequelize from '../db';

// modelName names the model (Archived -> Archiveds); no
// timestamps; a UUID primary key; a user createdAt wins over the default
// one.
class ArchivedRecord extends Model {}

ArchivedRecord.init(
  {
    uid: { type: DataTypes.UUID, primaryKey: true, defaultValue: DataTypes.UUIDV4 },
    createdAt: { type: DataTypes.DATE, allowNull: true },
  },
  { sequelize, modelName: 'Archived', timestamps: true, updatedAt: false },
);

export default ArchivedRecord;
