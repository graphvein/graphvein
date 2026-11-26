import { DataTypes, Model } from 'sequelize';

import sequelize from '../db';
import CustomDataTypes from './DataTypes';
import { amount, money, sized } from './helpers';

const SCALE = 2;

// Attribute helpers (row SQ-A1, form "helper member"; T018g3 Revision 2,
// open point O4): members of a default-exported helper, a member of a
// named helper, a whole helper result with a literal argument; and the
// refused forms (a member used as a value, an argument that is not a
// literal, a body with more than one statement).
class Wallet extends Model {}

Wallet.init(
  {
    currency: CustomDataTypes(DataTypes).currency,
    country: CustomDataTypes(DataTypes).country,
    flagged: CustomDataTypes(DataTypes).flagged,
    balance: money(DataTypes).amount,
    fee: amount(DataTypes, 4),
    rate: amount(DataTypes, SCALE),
    label: sized(DataTypes, true),
  },
  { sequelize },
);

export default Wallet;
