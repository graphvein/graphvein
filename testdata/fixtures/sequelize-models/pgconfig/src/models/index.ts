import { Sequelize } from 'sequelize';

import Account from './Account';
import Session from './Session';

// hasMany in the instance's schema: the key references "app"."Accounts".
Account.hasMany(Session);

export const version = Sequelize.version;

export { Account, Session };
