import express from 'express';
import { celebrate, Joi, Segments } from 'celebrate';
import { body } from 'express-validator';
import merge from 'lodash/merge';
import { CreateUser, Login, Profile } from './schemas';
import { createArticle, listArticles, postComment, postReview } from './handlers';
import { findUser, createOrder, sendNotFound, save, saveAll } from './services';
import { level1 } from './deep';

export const router = express.Router();

// SH-E1, SH-E4, SH-E7, SH-E8, SH-E23, SH-E25, SH-E28.
router.get('/users/:id', async (req, res) => {
  const include = req.query.include;
  const trace = req.get('X-Trace');
  const requestId = req.headers['x-request-id'];
  const session = req.cookies.session;
  if (req.params.id === 'none') {
    return res.status(404).json({ error: 'missing', code: 1 });
  }
  const user = await findUser(req.params.id);
  res.json(user);
});

// SH-E2 (no path parameter), SH-E24.
router.get('/health', (req, res) => res.sendStatus(204));

// SH-E12 zod parse.
router.post('/users', (req, res) => {
  const input = CreateUser.parse(req.body);
  res.status(201).json({ id: 'u1', name: input.name });
});

// SH-E13 zod safeParse with the result tested; D: an untested one.
router.post('/sessions', (req, res) => {
  const parsed = Login.safeParse(req.body);
  if (!parsed.success) {
    return res.status(400).json({ issues: parsed.error.issues });
  }
  res.status(201).json({ token: 't' });
});

router.post('/sessions/loose', (req, res) => {
  const parsed = Login.safeParse(req.body);
  console.log(parsed.success);
  res.json({ ok: true });
});

router.post('/articles', createArticle);
router.get('/articles', listArticles);
router.post('/articles/:slug/comments', postComment);
router.post('/reviews', postReview);

// SH-E15 celebrate.
router.put(
  '/articles/:slug',
  celebrate({ [Segments.BODY]: Joi.object({ title: Joi.string().required(), body: Joi.string() }) }),
  (req, res) => {
    res.status(200).json({ slug: req.params.slug });
  }
);

// SH-E16 express-validator.
router.post('/signup', body('email').isEmail(), body('password').isLength({ min: 8 }), (req, res) => {
  res.status(201).end();
});

// SH-E35 call following (depth 1 to 3), SH-E30 response passed on.
router.post('/orders', (req, res) => createOrder(req, res));

// Depth beyond the default limit (depth_cap).
router.post('/deep', (req, res) => level1(req.body, res));

// SH-E11 transformed values; SH-E35 unresolved and external callees.
router.post('/merge', (req, res) => {
  saveAll({ ...req.body, id: 1 });
  save(req.body.article);
  merge({}, req.body);
  const helper = makeHelper();
  helper.run(req.body);
  res.end();
});

function makeHelper() {
  return { run: (x: any) => x };
}

// SH-E29 next(err).
router.get('/fail', (req, res, next) => {
  next(new Error('boom'));
});

// SH-E26 redirects.
router.get('/old', (req, res) => res.redirect('/new'));
router.get('/moved', (req, res) => res.redirect(301, '/new'));

// SH-E31 a status not read; a status set apart from the send.
router.get('/computed', (req, res) => {
  const code = Number(req.query.code);
  res.status(code).json({ code });
});
router.get('/accepted', (req, res) => {
  res.status(202);
  res.send('queued');
});

// SH-E30 proven helper on the response; SH-E6 a computed query key.
router.get('/missing/:name', (req, res) => {
  const key = req.params.name;
  const value = req.query[key];
  sendNotFound(res);
});

// SH-E36 a handler that is not a proven function.
router.get('/wrapped', wrap(postReview));

function wrap(h: any) {
  return h;
}

// D: a response method no row reads.
router.get('/file', (req, res) => {
  res.download('/tmp/report.pdf');
});

// SH-E19: a validation whose failure may not stop the request is
// intended: tested in a helper whose result the handler ignores, or
// thrown inside a try block.
router.post('/profiles', (req, res) => {
  checkProfile(req.body);
  res.status(201).end();
});

function checkProfile(input: unknown) {
  const result = Profile.safeParse(input);
  if (!result.success) {
    return false;
  }
  return true;
}

router.post('/tries', (req, res) => {
  try {
    CreateUser.parse(req.body);
  } catch (e) {
    console.log(e);
  }
  res.end();
});

// SH-E6: the body is replaced before it is read; nothing is claimed.
router.post('/replaced', (req, res) => {
  req.body = { fixed: 1 };
  res.json({ fixed: req.body.fixed });
});

// SH-E27: an object with toJSON sends what toJSON returns.
router.get('/custom', (req, res) => {
  res.json({ a: 1, toJSON() { return { b: 2 }; } });
});

// SH-E25: a status set after the send is ignored by Express; the send
// is not claimed as 200 either (a status call stands apart).
router.get('/late', (req, res) => {
  res.json({ a: 1 });
  res.status(500);
});
