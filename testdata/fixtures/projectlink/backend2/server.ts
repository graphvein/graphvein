import express from 'express';

function users(req: unknown, res: { end(): void }) { res.end(); }
function user(req: unknown, res: { end(): void }) { res.end(); }

export const app = express();
app.get('/t022/users', users);
app.get('/t022/users/:id', user);
