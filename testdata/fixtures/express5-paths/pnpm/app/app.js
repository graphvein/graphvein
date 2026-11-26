// `catalog:` names the default pnpm catalog (Express 5,
// EX5-W12).
import express from 'express';
import { reply } from './reply.js';

export const app = express();
app.get('/pnpm/*splat', reply('pnpm-splat'));
