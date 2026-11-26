import { DataTypes, Model } from 'sequelize';

import sequelize from '../db';

// Every type and column form of rows SQ-A1 and SQ-A2 on postgres
// (investigation probe 1); the table name is inflection's plural (People).
class Person extends Model {
  declare id: number;
}

Person.init(
  {
    id: { type: DataTypes.INTEGER, primaryKey: true, autoIncrement: true },
    nick: { type: DataTypes.STRING, allowNull: false, unique: true },
    title: DataTypes.STRING(40),
    code: { type: DataTypes.CHAR(4), defaultValue: 'abcd' },
    bio: { type: DataTypes.TEXT, comment: 'free text', validate: { len: [0, 500] } },
    visits: { type: DataTypes.INTEGER, defaultValue: 0, allowNull: false },
    big: { type: DataTypes.BIGINT, defaultValue: -3 },
    small: DataTypes.SMALLINT,
    ratio: { type: DataTypes.FLOAT, defaultValue: 1.5 },
    score: { type: DataTypes.DOUBLE, defaultValue: 2 },
    amount: { type: DataTypes.DECIMAL(10, 2), defaultValue: 1.25 },
    plain: DataTypes.DECIMAL,
    active: { type: DataTypes.BOOLEAN, defaultValue: true },
    bornAt: DataTypes.DATE,
    bornOn: DataTypes.DATEONLY,
    wakesAt: DataTypes.TIME,
    token: { type: DataTypes.UUID, defaultValue: DataTypes.UUIDV4 },
    prefs: { type: DataTypes.JSON, defaultValue: { theme: 'dark', sizes: [1, 2] } },
    flags: { type: DataTypes.JSONB, defaultValue: '{}' },
    avatar: DataTypes.BLOB,
    mood: { type: DataTypes.ENUM('happy', 'sad'), defaultValue: 'happy' },
    level: { type: DataTypes.ENUM, values: ['low', 'high'] },
    tags: DataTypes.ARRAY(DataTypes.STRING),
    lucky: DataTypes.ARRAY(DataTypes.INTEGER),
    moods: DataTypes.ARRAY(DataTypes.ENUM('calm', 'busy')),
    stay: DataTypes.RANGE(DataTypes.DATE),
    region: { type: DataTypes.STRING, unique: 'nick_region' },
    zone: { type: DataTypes.STRING, unique: 'nick_region' },
    email: { type: DataTypes.STRING, unique: { name: 'person_email', msg: 'taken' } },
    seenAt: { type: DataTypes.DATE, defaultValue: DataTypes.NOW },
    nothing: { type: DataTypes.STRING, defaultValue: null },
    slug: { type: DataTypes.STRING, defaultValue: sequelize.literal("'x'") },
    stamp: { type: DataTypes.DATE, defaultValue: sequelize.fn('now') },
    picked: { type: DataTypes.STRING, defaultValue: () => 'later' },
    legal: { type: DataTypes.STRING, field: 'legal_name' },
    fullName: {
      type: DataTypes.VIRTUAL,
      get() {
        return 'x';
      },
    },
  },
  { sequelize, hooks: { beforeSave: () => undefined }, scopes: {}, defaultScope: {} },
);

export default Person;
