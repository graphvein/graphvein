import express from 'express';
import cookieParser from 'cookie-parser';
import { router } from './routes';
import { plain } from './plain';
import { legacy } from './legacy';
import { probes } from './probes';

export const app = express();

// Mounted before the parsers: no body parser, no cookie-parser (SH-E9, SH-E33).
app.use('/raw', plain);
app.use(express.json());
app.use(cookieParser());
app.use('/api', router);
app.use('/legacy', legacy);
// A known library registered only conditionally leaves the routes after
// it as written (SH-E25).
if (process.env.ORACLE_COOKIES) {
  app.use(cookieParser());
}
app.use('/rv', probes);
