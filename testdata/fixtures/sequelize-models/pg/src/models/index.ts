import { Sequelize } from 'sequelize';

import Archived from './Archived';
import Category from './Category';
import Mouse from './Mouse';
import Person from './Person';
import Settlement from './Settlement';
import Spread from './Spread';
import Unread from './Unread';
import Wallet from './Wallet';

// One association (rows SQ-R1): the holder's column set is
// partial until then.
Mouse.belongsTo(Person);

// The sequelize import puts the module in the model payload (gate row
// G4), so the association call is read at module level.
export const version = Sequelize.version;

export { Archived, Category, Mouse, Person, Settlement, Spread, Unread, Wallet };
