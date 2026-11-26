// Earlier route handlers (DA-E1, DA-E4, DA-E5; T026 AG-RF): route layers
// pushed before a route run first when their method and path match, and
// route(p).all(...) handlers run before the methods chained after them.
// Syntax valid in Express 4 and 5.
import express from 'express';
import { audit, requireAdmin, trace, reply, onError } from './mw.js';

export const chained = express();
chained.route('/items').all(requireAdmin).get(reply('items-get'));
chained.route('/things').get(reply('things-get')).all(audit).post(reply('things-post'));
chained.all('/open', trace('all-open'));
chained.get('/open', reply('open'));
chained.all('/other/x', trace('other-x'));
chained.get('/other', reply('other'));
chained.use(onError);
chained.use((err, req, res, next) => next(err));
chained.get('/after-error', reply('after-error'));
chained.get('/:id', trace('by-id'));
chained.get('/feed', reply('feed'));

// An earlier sibling router at the same prefix whose route may answer
// first.
export const sib = express();
const first = express.Router();
first.get('/:slug', trace('first-slug'));
const second = express.Router();
second.get('/feed', reply('sib-feed'));
sib.use('/', first);
sib.use('/', second);
