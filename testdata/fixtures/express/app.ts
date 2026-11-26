import express from 'express';
import { router, BASE, makeRouter } from './router';
import subapp from '@example/subapp';
import { Router } from 'lookalike';
import http from 'axios';
export const app = express();
app.use(BASE, router);
app.use('/second', router);
if (enabled) app.use('/conditional', router);
app.use('/factory', makeRouter());
app.use(subapp);
app.use(prefix + '/dynamic', router);
const fake = Router();
fake.get('/fake', handler);
http.get('/api/x');
export const sub = express();
sub.get('/sub', (req, res) => res.end());
app.use('/child', sub);
function shadow(express: unknown) {
  const other = express();
  other.get('/not-express', handler);
}
import { Router as LocalRouter } from './lookalike';
const localFake = LocalRouter();
localFake.get('/local-fake', handler);
