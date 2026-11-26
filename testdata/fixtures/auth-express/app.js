import express from 'express';
import { logger, audit, trace, reply, stack, pathOf } from './mw.js';
import { api } from './api.js';
import { admin, guarded } from './admin.js';
import { configure } from './setup.js';

export const app = express();
app.use(logger);
app.get('/early', reply('early'));
app.use(audit);
app.use('/api', trace('api-gate'), api);
app.use('/admin', admin);
app.use('/API2', trace('case'));
app.get('/api2/z', reply('api2'));
app.get('/late', reply('late'));
app.use('/late', trace('after-late'));
configure(app);
if (process.env.GRAPHVEIN_ORACLE_FLAG) {
  app.use(trace('conditional'));
}
app.use(...stack);
app.use(pathOf(), trace('maybe-path'));
app.use(['/a', '/b'], trace('array-path'));
app.get('/after-escape', reply('after-escape'));
app.use(guarded);
app.get('/after-guarded', reply('after-guarded'));


// A use inside a function body: its order relative to the routes is not
// read (another app, so the other routes stay exact for the oracle).
export const other = express();
other.get('/before-function', reply('before-function'));
function later() {
  other.use(trace('in-function'));
}
later();
other.get('/after-function', reply('after-function'));

export const sub = express();
sub.use(trace('sub-own'));
sub.get('/hello', reply('sub-hello'));
app.use('/sub', sub);
export { cfg, twice, later, alias, pass } from './configured.js';
