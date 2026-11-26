// A nested package.json without express: the walk goes on to the parent,
// which declares Express 5 (EX5-W12).
import express from 'express';
import { reply } from './reply.js';

export const app = express();
app.get('/nested/*splat', reply('nested-splat'));
