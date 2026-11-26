// Auth library factories (row DA-E2). Not run by the oracle: the
// packages are syntax only here.
import express from 'express';
import passport from 'passport';
import { expressjwt } from 'express-jwt';
import jwtV6 from 'express-jwt';
import { auth } from 'express-oauth2-jwt-bearer';
import { requiresAuth } from 'express-openid-connect';
import { reply } from './mw.js';
import { options, strategyName } from './options.js';

const checks = {
  required: expressjwt({ secret: 's', algorithms: ['HS256'] }),
  optional: expressjwt({ secret: 's', algorithms: ['HS256'], credentialsRequired: false }),
};

function ensureLoggedIn(req, res, next) {
  next();
}

export const libs = express();
libs.use('/session', passport.authenticate('session'));
libs.get('/session/me', reply('session-me'));
libs.get('/jwt', checks.required, reply('jwt'));
libs.get('/jwt-optional', checks.optional, reply('jwt-optional'));
libs.get('/v6', jwtV6({ secret: 's', credentialsRequired: options.flag }), reply('v6'));
libs.use('/bearer', auth({ authRequired: false }));
libs.get('/bearer/x', reply('bearer'));
libs.get('/oidc', requiresAuth(), reply('oidc'));
libs.get('/strategy', passport.authenticate(strategyName), reply('strategy'));
libs.get('/custom', ensureLoggedIn, reply('custom'));
