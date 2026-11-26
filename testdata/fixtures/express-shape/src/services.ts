import { User, NewComment } from './types';

export async function findUser(id: string): Promise<User> {
  return { id, name: 'n', roles: [] };
}

export function addComment(input: NewComment) {
  return { saved: input.body };
}

export function createOrder(req: any, res: any) {
  const { items } = req.body;
  validateOrder(req.body);
  res.status(201).json({ count: items.length });
}

function validateOrder(order: any) {
  if (order.total < 0) {
    throw new Error('negative');
  }
  auditOrder(order);
}

function auditOrder(order: any) {
  return order.customer;
}

export function sendNotFound(res: any) {
  res.status(404).end();
}

export function save(article: any) {
  return article.title;
}

export function saveAll(data: any) {
  return data;
}
