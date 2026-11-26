import express, { NextFunction, Request, Response } from 'express';
import { celebrate, Joi, Segments } from 'celebrate';
import { Item, JNum, JStrict, JStrictAll, JOrder, JEmpty, JIns, JPlain } from './schemas';
import { Body13 } from './types';

// Further probes of the Express shape rows. Every route answers
// 201 when its handler runs to the end; a validation that stops the
// request answers 400 or 500 (a thrown error).
export const probes = express.Router();

const strictMode = process.env.STRICT === '1';

// SH-E19: a throwing validation that is not a top-level statement before
// every send, or not awaited, does not stop the request: intended.
probes.post('/p1', (req, res) => {
  if (req.query.strict) {
    Item.parse(req.body);
  }
  res.status(201).json({ ok: true });
});
probes.post('/p2', (req, res) => {
  Item.parseAsync(req.body);
  res.status(201).json({ ok: true });
});
probes.post('/p2c', (req, res) => {
  Item.parseAsync(req.body).catch(() => undefined);
  res.status(201).json({ ok: true });
});
async function check(b: unknown) {
  Item.parse(b);
}
probes.post('/p2b', (req, res) => {
  check(req.body);
  res.status(201).json({ ok: true });
});
probes.post('/p4', (req, res) => {
  res.status(201).json({ ok: true });
  Item.parse(req.body);
});
probes.post('/p17', (req, res) => {
  for (const k of Object.keys(req.query)) {
    Item.parse(req.body);
  }
  res.status(201).json({ ok: true });
});
probes.post('/p18', (req, res) => {
  req.query.skip || Item.parse(req.body);
  res.status(201).json({ ok: true });
});

// SH-E12: awaited at the top level, directly and through an awaited
// async helper: enforced.
probes.post('/p19', async (req, res) => {
  const v = await Item.parseAsync(req.body);
  res.status(201).json({ name: v.name });
});
probes.post('/p2d', async (req, res) => {
  await check(req.body);
  res.status(201).json({ ok: true });
});

// SH-E19: a result tested with &&, inside a callback, or by the success
// test: not tested.
probes.post('/p3', (req, res) => {
  const r = Item.safeParse(req.body);
  if (r.error && strictMode) {
    return res.status(400).json({ bad: true });
  }
  res.status(201).json({ ok: true });
});
probes.post('/p3b', (req, res) => {
  const r = Item.safeParse(req.body);
  [1].forEach(() => {
    if (!r.success) return;
  });
  res.status(201).json({ ok: true });
});
probes.post('/p3c', (req, res) => {
  const r = Item.safeParse(req.body);
  if (r.success) {
    return res.status(201).json({ ok: true });
  }
  res.status(201).json({ ok: false });
});

// SH-E13: the failure test as an operand of ||: tested.
probes.post('/p3d', (req, res) => {
  const r = Item.safeParse(req.body);
  if (!r.success || strictMode) {
    return res.status(400).json({ bad: true });
  }
  res.status(201).json({ ok: true });
});

// SH-E19: a call on the response between the validation and its test.
probes.post('/p3e', (req, res) => {
  const { error } = JPlain.validate(req.body);
  res.set('X-Checked', 'yes');
  if (error) {
    return res.status(400).json({ bad: true });
  }
  res.status(201).json({ ok: true });
});

// SH-E14, SH-E21: Joi converts; strict() on the key or the object stops it.
probes.post('/p5', (req, res) => {
  const { error } = JNum.validate(req.body);
  if (error) {
    return res.status(400).json({ e: error.message });
  }
  res.status(201).json({ ok: true });
});
probes.post('/p5s', (req, res) => {
  const { error } = JStrict.validate(req.body);
  if (error) {
    return res.status(400).json({ e: error.message });
  }
  res.status(201).json({ ok: true });
});
probes.post('/p5a', (req, res) => {
  const { error } = JStrictAll.validate(req.body);
  if (error) {
    return res.status(400).json({ e: error.message });
  }
  res.status(201).json({ ok: true });
});

// SH-E21: presence modifiers, empty(null), insensitive().
probes.post('/p6', (req, res) => {
  const { error } = JOrder.validate(req.body);
  if (error) {
    return res.status(400).json({ e: error.message });
  }
  res.status(201).json({ ok: true });
});
probes.post('/p7', (req, res) => {
  const { error } = JEmpty.validate(req.body);
  if (error) {
    return res.status(400).json({ e: error.message });
  }
  res.status(201).json({ ok: true });
});
probes.post('/p8', (req, res) => {
  const { error } = JIns.validate(req.body);
  if (error) {
    return res.status(400).json({ e: error.message });
  }
  res.status(201).json({ ok: true });
});

// SH-E15: celebrate options change presence; without them the schema
// stands (Express 4 with express.json mounted: req.body is {}).
probes.post('/p9', celebrate({ [Segments.BODY]: Joi.object({ a: Joi.string(), b: Joi.string() }) }, { presence: 'required' }), (req, res) => {
  res.status(201).json({ ok: true });
});
probes.post('/q1', (req, res) => {
  const { error } = JPlain.validate(req.body);
  if (error) {
    return res.status(400).json({ bad: true });
  }
  res.status(201).json({ ok: true });
});

// SH-E23: res.json keeps a Content-Type set before it; 204 has none.
probes.get('/p10', (req, res) => {
  res.status(200).type('text/plain').json({ a: 1 });
});
probes.get('/p11', (req, res) => {
  res.status(204).json({ a: 1 });
});
probes.get('/p12', (req, res) => {
  res.set('Content-Type', 'text/plain');
  res.status(200).json({ a: 1 });
});

// SH-E25: a middleware before the handler sets the status.
probes.get('/p16', (req: Request, res: Response, next: NextFunction) => {
  res.status(202);
  next();
}, (req, res) => {
  res.json({ ok: true });
});

// SH-E22: `T | undefined` is optional, not nullable.
probes.post('/p13', (req: Request<{}, any, Body13>, res) => {
  res.status(201).json({ a: req.body.a });
});

// SH-E7: req.headers keys are lower case; req.get is not case-sensitive.
probes.get('/p14', (req, res) => {
  const t = req.headers['X-Token'];
  const u = req.headers['x-token'];
  res.status(200).json({ t: t ?? null, u: u ?? null });
});

// SH-E12: a send in an earlier top-level if that always returns does not
// precede the validation on the path that reaches it: enforced.
probes.post('/p20', (req, res) => {
  if (req.query.off) {
    return res.status(404).json({ off: true });
  }
  const v = Item.parse(req.body);
  res.status(201).json({ name: v.name });
});

// SH-E19: an early exit that answers 2xx lets a request succeed without
// the validation after it: intended.
probes.post('/a1', (req, res) => {
  if (req.query.cached) {
    return res.status(200).json({ cached: true });
  }
  Item.parse(req.body);
  res.status(201).json({ ok: true });
});

// SH-E25: a middleware that sets the Content-Type, and one that replaces
// res.json: neither the 200 default, the content type nor the body of the
// handler's send is claimed.
function plainText(req: Request, res: Response, next: NextFunction) {
  res.type('text/plain');
  next();
}
probes.get('/b1', plainText, (req, res) => {
  res.status(200).json({ a: 1 });
});
probes.use('/b2', (req: Request, res: Response, next: NextFunction) => {
  const orig = res.json.bind(res);
  res.json = (v: any) => orig({ data: v, wrapped: true });
  next();
});
probes.get('/b2', (req, res) => {
  res.json({ a: 1 });
});

// SH-E25: middleware whose only response use is a send that ends the
// request leaves the handler's response claimed: an inline check and the
// function a factory returns.
function requireRole(role: string) {
  const check = (req: Request, res: Response, next: NextFunction) => {
    if (req.get('x-role') !== role) {
      res.status(403).json({ forbidden: true });
    } else {
      next();
    }
  };
  return check;
}
probes.get('/b3', (req: Request, res: Response, next: NextFunction) => {
  if (!req.get('authorization')) {
    return res.status(401).json({ unauthorized: true });
  }
  next();
}, (req, res) => {
  res.json({ me: 1 });
});
probes.get('/b4', requireRole('admin'), (req, res) => {
  res.json({ admin: 1 });
});

// SH-E25: with an explicit status the replaced res.json still changes the
// body: no body or content type is claimed.
probes.use('/b5', (req: Request, res: Response, next: NextFunction) => {
  const orig = res.json.bind(res);
  res.json = (v: any) => orig({ data: v, wrapped: true });
  next();
});
probes.get('/b5', (req, res) => {
  res.status(200).json({ a: 1 });
});
