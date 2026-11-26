import express, { Router as NewRouter } from 'express';
import PromiseRouter from 'express-promise-router';
import { handle } from './handlers';
import { hidden } from './barrel';
export const BASE = '/api//';
export const router = NewRouter();
const nested = express.Router();
router.use(nested);
nested.route('/users/:id?').get(auth, handle).post((req, res) => res.end());
nested.all('/files/*', handle);
nested.get('/same', handle);
nested.get('/same', handle);
nested.get(variable, handle);
nested.get('/barrel', hidden);
const lonely = PromiseRouter();
lonely.head('/alone', handle);
export function makeRouter() {
  const result = PromiseRouter();
  result.put(`/factory`, handle);
  return result;
}
