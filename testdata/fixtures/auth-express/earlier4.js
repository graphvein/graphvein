// Wildcard route layers in Express 4 syntax (path-to-regexp 0.1: `*`):
// the example of Express's own app.all documentation. Loaded by the
// oracle for Express 4 only (Express 5 refuses an unnamed `*`).
import express from 'express';
import { trace, reply } from './mw.js';

export const star4 = express();
const inner = express.Router();
inner.all('*', trace('router-all'));
inner.get('/other', reply('star4-other'));
star4.all('/api/*', trace('api-all'));
star4.get('/api/open', reply('star4-api-open'));
star4.use('/r', inner);
