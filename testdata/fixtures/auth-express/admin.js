import express from 'express';
import { requireAdmin, reply } from './mw.js';

export const admin = express.Router();
admin.use(requireAdmin);
admin.get('/users', reply('admin-users'));

export const guarded = express.Router();
guarded.use(trace2);
guarded.get('/guarded', reply('guarded'));

function trace2(req, res, next) {
  req.trace = req.trace || [];
  req.trace.push('trace2');
  next();
}
