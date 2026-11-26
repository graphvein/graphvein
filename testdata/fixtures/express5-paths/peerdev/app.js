// A peer range spanning both majors with a development range of one: the
// consumer's copy may be either major, so the major is unknown (EX5-W13);
// only what both read the same is claimed (EX5-W11).
import express from 'express';
import { reply } from './reply.js';

export const app = express();
app.get('/peer/users/:id', reply('peer-param'));
app.get('/peer/*splat', reply('peer-splat'));
