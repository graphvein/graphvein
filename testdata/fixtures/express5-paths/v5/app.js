// Express 5 path forms (package.json: express ^5.1.0, row EX5-W12).
// Every route sits under its own first segment so no two routes of one
// app match the same request.
import express from 'express';
import { reply } from './reply.js';

export const app = express();

// EX5-W5 literal text.
app.get('/l5/users/list.json', reply('v5-literal'));
// EX5-W6 a parameter filling its segment.
app.get('/p5/users/:id', reply('v5-param'));
// EX5-W7 a named wildcard ending the pattern.
app.get('/s5/files/*splat', reply('v5-splat'));
// EX5-W7 under a mount: the T028 case, `/r5` + `/*splat`.
const inner = express.Router();
inner.get('/*splat', reply('v5-mounted-splat'));
app.use('/r5', inner);
// EX5-W9 optional groups, nested and around a wildcard.
app.get('/g5/users{/:id}', reply('v5-group'));
app.get('/g5n/a{/b{/c}}', reply('v5-nested-group'));
app.get('/g5w/files{/*path}', reply('v5-group-wildcard'));

// EX5-W14: a parameter inside a segment, an escape.
app.get('/i5/:from-:to', reply('v5-in-segment'));
app.get('/e5/a\\:b', reply('v5-escape'));
// EX5-W14: a wildcard and a group in a mount prefix.
const rest = express.Router();
rest.get('/z', reply('v5-prefix-wildcard'));
app.use('/x5/*rest', rest);
const versioned = express.Router();
versioned.get('/z', reply('v5-prefix-group'));
app.use('/y5{/:v}', versioned);

// EX5-W14: group alternatives with an empty segment or a
// trailing slash Express 5 does not collapse.
app.get('/b1g/{*rest}', reply('v5-group-empty-wildcard'));
app.get('/b1h/{:id}', reply('v5-group-empty-param'));
app.get('/b1a/{/b}', reply('v5-group-double-slash'));
// EX5-W14: a piece without a leading slash matches nothing.
app.get(':b4id', reply('v5-no-slash'));
// A mount prefix with two trailing slashes (Express 4 makes
// one optional: no path; Express 5 drops them first: `/tp5`).
const trailing = express.Router();
trailing.get('/x', reply('v5-trailing-prefix'));
app.use('/tp5//', trailing);
// EX5-W14: literals no request path carries, an empty segment.
app.get('/c1v/a^b', reply('v5-caret'));
app.get('/c1v/é', reply('v5-non-ascii'));
app.get('/c1v//dbl', reply('v5-double-slash'));

// EX5-W8 a named wildcard as the whole pattern (Express 5 also matches
// `/`, and the bare mount path under a mount).
export const star = express();
star.get('*splat', reply('v5-root-splat'));
export const mounted = express();
const all = express.Router();
all.get('*splat', reply('v5-mounted-root-splat'));
mounted.use('/ms5', all);
