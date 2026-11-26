// The manifest starts with a UTF-8 byte order mark, which
// Node ignores; Graphvein reads it as Express 5 (EX5-W12).
import express from 'express';
import { reply } from './reply.js';

export const app = express();
app.get('/bom/*splat', reply('bom-splat'));
