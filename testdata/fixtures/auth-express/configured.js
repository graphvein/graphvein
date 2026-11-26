import express from 'express';
import { reply } from './mw.js';
import { configureAll, configureTwice, configureAsync, configureAlias, configurePass } from './setup.js';

// DA-E3 apps: each module function receives the app as a parameter.
export const cfg = express();
cfg.get('/cfg-before', reply('cfg-before'));
configureAll({ app: cfg, skip: false });
cfg.get('/cfg-after', reply('cfg-after'));

export const twice = express();
configureTwice(twice);
configureTwice(twice);
twice.get('/twice', reply('twice'));

export const later = express();
configureAsync(later);
later.get('/async', reply('async'));

export const alias = express();
configureAlias(alias);
alias.get('/alias', reply('alias'));

export const pass = express();
configurePass(pass);
pass.get('/pass', reply('pass'));
