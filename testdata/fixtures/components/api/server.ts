import express from 'express';

const app = express();

function auth(req: unknown, res: unknown, next: () => void) {
  next();
}

function items(req: unknown, res: { json(v: unknown): void }) {
  res.json([]);
}

function health(req: unknown, res: { json(v: unknown): void }) {
  res.json({ ok: true });
}

app.get('/t026/items', auth, items);
app.get('/t026/health', health);

export default app;
