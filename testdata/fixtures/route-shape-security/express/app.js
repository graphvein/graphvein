// Express routes of the route-shape-security fixture (T029):
// every auth library row and the middleware that keeps a section partial.
import express from 'express';
import cors from 'cors';
import { expressjwt } from 'express-jwt';
import { auth } from 'express-oauth2-jwt-bearer';
import basicAuth from 'express-basic-auth';
import passport from 'passport';
import { Strategy as JwtStrategy, ExtractJwt } from 'passport-jwt';
import { Strategy as BearerStrategy } from 'passport-http-bearer';
import { BasicStrategy } from 'passport-http';
import oidc from 'express-openid-connect';
import session from 'express-session';
import { Strategy as AnonymousStrategy } from 'passport-anonymous';
import { requireUser } from './custom.js';

export const SECRET = 'fixture-secret';

const verify = (payload, done) => done(null, payload);

passport.use(new JwtStrategy({ jwtFromRequest: ExtractJwt.fromAuthHeaderAsBearerToken(), secretOrKey: SECRET }, verify));
passport.use('header-jwt', new JwtStrategy({ jwtFromRequest: ExtractJwt.fromHeader('x-api-token'), secretOrKey: SECRET }, verify));
passport.use('query-jwt', new JwtStrategy({ jwtFromRequest: ExtractJwt.fromUrlQueryParameter('token'), secretOrKey: SECRET }, verify));
passport.use('upper-jwt', new JwtStrategy({ jwtFromRequest: ExtractJwt.fromHeader('X-Api-Token'), secretOrKey: SECRET }, verify));
passport.use('body-jwt', new JwtStrategy({ jwtFromRequest: ExtractJwt.fromBodyField('token'), secretOrKey: SECRET }, verify));
passport.use(new BearerStrategy((token, done) => done(null, token === 'bearer-token' ? { token } : false)));
passport.use('anonymous', new AnonymousStrategy());
passport.use(new BasicStrategy((user, pass, done) => done(null, user === 'user' && pass === 'pass' ? { user } : false)));

export const app = express();
app.use(cors());
app.use(express.json());
app.use(passport.initialize());

const jwtAuth = expressjwt({ secret: SECRET, algorithms: ['HS256'] });
const oauth = auth({ secret: SECRET, issuer: 'https://issuer.example', audience: 'fixture-api', tokenSigningAlg: 'HS256' });

function ok(req, res) {
  res.json({ ok: true });
}

app.get('/jwt', jwtAuth, ok);
app.get('/jwt-optional', expressjwt({ secret: SECRET, algorithms: ['HS256'], credentialsRequired: false }), ok);
app.get('/jwt-custom', expressjwt({ secret: SECRET, algorithms: ['HS256'], getToken: (req) => req.query.t }), ok);
app.get('/oauth', oauth, ok);
app.get('/oauth-optional', auth({ secret: SECRET, issuer: 'https://issuer.example', audience: 'fixture-api', tokenSigningAlg: 'HS256', authRequired: false }), ok);
app.get('/basic', basicAuth({ users: { user: 'pass' } }), ok);
app.get('/passport-jwt', passport.authenticate('jwt', { session: false }), ok);
app.get('/passport-header', passport.authenticate('header-jwt', { session: false }), ok);
app.get('/passport-query', passport.authenticate('query-jwt', { session: false }), ok);
app.get('/passport-upper', passport.authenticate('upper-jwt', { session: false }), ok);
app.get('/passport-body', passport.authenticate('body-jwt', { session: false }), ok);
app.get('/passport-bearer', passport.authenticate('bearer', { session: false }), ok);
app.get('/passport-basic', passport.authenticate('basic', { session: false }), ok);
app.get('/passport-unknown', passport.authenticate('nosuch', { session: false }), ok);
app.get('/passport-callback', passport.authenticate('jwt', { session: false }, () => {}), ok);
app.get('/stacked', jwtAuth, passport.authenticate('header-jwt', { session: false }), ok);
app.get('/custom', requireUser, ok);
app.get('/mixed', requireUser, jwtAuth, ok);
app.get('/oidc', oidc.requiresAuth(), ok);
app.get('/many', oauth, oauth, oauth, oauth, oauth, ok);

const api = express.Router();
api.use(jwtAuth);
api.get('/items', ok);
app.use('/api', api);

if (process.env.GV_FIXTURE_AUTH) {
  app.use('/cond', basicAuth({ users: { user: 'pass' } }));
}
app.get('/cond/jwt', jwtAuth, ok);

// Further forms (rows SH-S6, SH-S10): the built-in session strategy, a
// strategy list, a strategy whose class no row reads and requiresAuth
// with a requiresLoginCheck argument.
const sessions = session({ secret: SECRET, resave: false, saveUninitialized: false });
app.get('/p-session', sessions, passport.authenticate('session'), ok);
app.get('/p-anon', passport.authenticate(['jwt', 'anonymous'], { session: false }), ok);
app.get('/p-anon-name', passport.authenticate('anonymous', { session: false }), ok);
app.get('/oidc-check', oidc.requiresAuth(() => false), ok);
