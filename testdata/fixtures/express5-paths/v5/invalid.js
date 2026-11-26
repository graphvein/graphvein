// EX5-W10: patterns path-to-regexp 8 refuses; Express 5 throws at each
// registration, so this module never loads. The oracle registers each
// pattern on its own and checks that Express 5 throws.
import express from 'express';
import { reply } from './reply.js';

export const invalid = express();
invalid.get('/v5/users/:id?', reply('v5-optional'));
invalid.get('/v5/files/*', reply('v5-star'));
invalid.get('/v5/a(b)', reply('v5-parenthesis'));
invalid.get('/v5/:a{:b}', reply('v5-missing-text'));
