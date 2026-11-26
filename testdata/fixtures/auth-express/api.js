import { Router } from 'express';
import { trace, reply } from './mw.js';

export const api = Router();
api.get('/public', reply('api-public'));
api.use('/items', trace('items-only'));
api.get('/items/:id', reply('api-item'));
api.get('/itemsx', reply('api-itemsx'));
api.get('/:kind/list', reply('api-kind-list'));
api.use('/:tenant/x', trace('tenant'));
api.get('/acme/x/y', reply('api-tenant'));
