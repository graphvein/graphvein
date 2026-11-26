import express from 'express';
import { db } from './db/client';
import { users } from './db/schema';

async function listUsers(req: unknown, res: { json(v: unknown): void }) {
  res.json(await db.select().from(users));
}
async function createUser(req: { body: { email: string } }, res: { json(v: unknown): void }) {
  res.json(await db.insert(users).values({ email: req.body.email }));
}

export const app = express();
app.get('/t022b7/users', listUsers);
app.post('/t022b7/users', createUser);
