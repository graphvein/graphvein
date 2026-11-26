import express from 'express';

function currentUser(req: unknown, res: { json(v: unknown): void }) {
  res.json({ user: null });
}
function listArticles(req: unknown, res: { json(v: unknown): void }) {
  res.json([]);
}
function login(req: unknown, res: { json(v: unknown): void }) {
  res.json({ token: '' });
}
function health(req: unknown, res: { json(v: unknown): void }) {
  res.json({ ok: true });
}

export const app = express();
app.get('/t022r/user', currentUser);
app.get('/t022r/articles', listArticles);
app.post('/t022r/login', login);
app.get('/t022r/health', health);
