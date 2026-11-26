// `catalog:legacy` names a named pnpm catalog (Express 4,
// EX5-W12).
import express from 'express';
import { reply } from './reply.js';

export const app = express();
app.get('/legacy/*', reply('legacy-star'));
