import { SUPPORTED_CURRENCIES } from './currencies';

// opencollective-api's attribute helper shape (server/models/DataTypes.ts,
// written anew): a default-exported function of the DataTypes namespace
// that returns one object literal, and imports nothing from Sequelize
// (payload gate row G9). Row SQ-A1, form "helper member".
export default function (DataTypes) {
  return {
    // Read: STRING(3), not null, default 'USD'; validate and set have no
    // table fact.
    currency: {
      type: DataTypes.STRING(3),
      defaultValue: 'USD',
      validate: {
        len: [3, 3] as [number, number],
        isIn: {
          args: [SUPPORTED_CURRENCIES],
          msg: 'Currency not supported',
        },
      },
      allowNull: false,
      set(val) {
        if (val && val.toUpperCase) {
          this.setDataValue('currency', val.toUpperCase());
        }
      },
    },
    // Not read: the parameter is used as a value, not as the namespace.
    flagged: { type: DataTypes.BOOLEAN, allowNull: Boolean(DataTypes) },
    // Read, but the default is a name of this module: unknown.
    country: { type: DataTypes.STRING(2), defaultValue: SUPPORTED_CURRENCIES[0] },
  };
}
