// A workspace member without express; the workspace root declares
// Express 5 (EX5-W12).
import express from 'express';
import { reply } from './reply.js';

export const app = express();
app.get('/ws/*splat', reply('ws-splat'));
