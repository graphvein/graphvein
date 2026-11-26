// A manifest that does not parse stops the walk: the major is
// unknown (EX5-W13), although the parent declares Express 5. Node refuses
// this package.json too, so the oracle does not load this module.
import express from 'express';
import { reply } from './reply.js';

export const app = express();
app.get('/broken/*splat', reply('broken-splat'));
app.get('/broken/users/:id', reply('broken-param'));
