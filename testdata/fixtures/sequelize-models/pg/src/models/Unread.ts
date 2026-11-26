import { DataTypes, Model } from 'sequelize';

import sequelize from '../db';
import { sized } from './helpers';

// A project data type (loadable: Sequelize renders an ABSTRACT subclass
// by its instance key, as opencollective-api's ExpenseTypesEnum; this one
// renders INTEGER).
class Cents extends DataTypes.ABSTRACT {
  key = 'INTEGER';
}

const States = { OPEN: 'open' } as const;

// Columns the contract does not read (unknown, open point
// O4): a `new` VIRTUAL, a helper whose body is not one return, a project
// type, a raw string type, an unread argument and an unread key; a default
// that is an enum member (open point O5).
class Unread extends Model {}

Unread.init(
  {
    label: { type: DataTypes.STRING, allowNull: false },
    shown: new DataTypes.VIRTUAL(DataTypes.STRING, ['label']),
    price: sized(DataTypes, false),
    cents: { type: new Cents() },
    raw: { type: 'VARCHAR(12)' },
    wide: DataTypes.INTEGER(11),
    ident: { type: DataTypes.INTEGER, autoIncrementIdentity: true },
    state: { type: DataTypes.STRING, defaultValue: States.OPEN },
  },
  { sequelize, freezeTableName: true },
);

export default Unread;
