// The Express major is unknown here (package.json: express
// `^4.21.1 || ^5.1.0`, row EX5-W13): a path is claimed only where Express
// 4 and 5 read it the same (EX5-W11).
import express from 'express';
import { reply } from './reply.js';

export const app = express();

app.get('/l/users', reply('u-literal'));
app.get('/p/users/:id', reply('u-param'));
const mounted = express.Router();
mounted.get('/x', reply('u-mounted'));
app.use('/m', mounted);

// EX5-W14: Express 4 and 5 read these differently.
app.get('/s/*splat', reply('u-splat'));
app.get('/g/users{/:id}', reply('u-group'));
