// Express 4 path forms (package.json: express 4.21.1, row EX5-W12).
// Every route sits under its own first segment so no two routes of one
// app match the same request.
import express from 'express';
import { reply } from './reply.js';

export const app = express();

// EX5-W1 literal text (the dot is literal in Express 4 too).
app.get('/l4/users/list.json', reply('v4-literal'));
// EX5-W2 a parameter filling its segment.
app.get('/p4/users/:id', reply('v4-param'));
// EX5-W3 an optional parameter, final and inside the path.
app.get('/o4/users/:id?', reply('v4-optional'));
app.get('/o4mid/:id?/edit', reply('v4-optional-middle'));
// EX5-W4 `*` ending the pattern.
app.get('/w4/files/*', reply('v4-star'));
// EX5-W2 in a mount prefix, EX5-W4 under it.
const org = express.Router();
org.get('/*', reply('v4-mounted-star'));
app.use('/m4/:org', org);

// EX5-W14: `/*splat` is `.*` followed by the literal `splat` in Express 4.
app.get('/s4/*splat', reply('v4-splat'));
// EX5-W14: regular expression syntax and a parameter inside a segment.
app.get('/q4/ab?cd', reply('v4-regex'));
app.get('/f4/:file.:ext', reply('v4-in-segment'));
// EX5-W14: a wildcard in a mount prefix.
const rest = express.Router();
rest.get('/z', reply('v4-prefix-star'));
app.use('/x4/*', rest);

// EX5-W3: a non-final optional parameter in a pattern and in a
// mount prefix claims only its present form.
app.get('/b2a/:id?/x/:y', reply('v4-optional-inner'));
const optional = express.Router();
optional.get('/x/:id', reply('v4-optional-prefix'));
app.use('/b2m/:org?', optional);
// EX5-W14: pieces without a leading slash match nothing.
app.get('b4users', reply('v4-no-slash'));
const bare = express.Router();
bare.get('/x', reply('v4-no-slash-prefix'));
app.use('b4api', bare);
// A mount prefix with two trailing slashes (Express 4 makes
// one optional: no path; Express 5 drops them first: `/tp4`).
const trailing = express.Router();
trailing.get('/x', reply('v4-trailing-prefix'));
app.use('/tp4//', trailing);
// EX5-W14: an empty segment and a space.
app.get('/c1//dbl', reply('v4-double-slash'));
app.get('/c1/a b', reply('v4-space'));

// EX5-W4 as the whole pattern of a root app (Express 4 also matches `/`).
export const star = express();
star.get('*', reply('v4-root-star'));
