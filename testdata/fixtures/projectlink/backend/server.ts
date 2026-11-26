import express from 'express';

function users(req: unknown, res: { end(): void }) { res.end(); }
function user(req: unknown, res: { end(): void }) { res.end(); }
function order(req: unknown, res: { end(): void }) { res.end(); }
function first(req: unknown, res: { end(): void }) { res.end(); }
function second(req: unknown, res: { end(): void }) { res.end(); }
function same(req: unknown, res: { end(): void }) { res.end(); }

export const app = express();
app.get('/t022/users', users);
app.get('/t022/users/:id', user);
app.post('/t022/orders', order);
// Two registrations with different handlers: ambiguous in this repository
// and across repositories.
app.get('/t022/duplicate', first);
app.get('/t022/duplicate', second);
// Two equivalent registrations (one app, method, pattern, handler and
// middleware): the first links, inside one repository and across
// repositories (ADR 0022, stored as equivalent_registrations).
app.get('/t022/same', same);
app.get('/t022/same', same);
