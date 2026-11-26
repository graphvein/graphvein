// The orders server of the monorepo member (link scope "orders"). It also
// registers GET /t022/users and GET /t022/mono/health, so calls to them are
// ambiguous inside the repository.
import express from 'express';

function users(req: unknown, res: { end(): void }) { res.end(); }
function order(req: unknown, res: { end(): void }) { res.end(); }
function health(req: unknown, res: { end(): void }) { res.end(); }

export const app = express();
app.get('/t022/users', users);
app.post('/t022/orders', order);
app.get('/t022/mono/health', health);
app.listen(3102);
