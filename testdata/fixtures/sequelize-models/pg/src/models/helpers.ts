import { Sequelize } from 'sequelize';

// Attribute helpers (row SQ-A1, form "helper member"): named function
// declarations (payload row S6).
export function money(types) {
  return { amount: { type: types.INTEGER, allowNull: false } };
}

export function amount(types, scale) {
  return { type: types.DECIMAL(10, scale), allowNull: false, defaultValue: 0 };
}

// Not read: a body other than a single return.
export function sized(types, wide) {
  if (wide) {
    return { type: types.TEXT };
  }
  return { type: types.STRING };
}

// A model built in a function (row D: a marker, the table list may be
// incomplete).
export function makeTemp(s: Sequelize) {
  return s.define('Temp', { note: Sequelize.STRING });
}
