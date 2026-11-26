// Wildcard route layers in Express 5 syntax (path-to-regexp 8: a named
// `*splat`). Loaded by the oracle for Express 5 only; its own
// package.json declares Express 5, so Graphvein reads its paths as
// Express 5 does (T028 row EX5-W12).
import express from 'express';
import { trace, reply } from '../mw.js';

export const star5 = express();
const inner = express.Router();
inner.all('/*splat', trace('router-all'));
inner.get('/other', reply('star5-other'));
star5.all('/api/*splat', trace('api-all'));
star5.get('/api/open', reply('star5-api-open'));
star5.use('/r', inner);
