import express from 'express';
const app = express();
export function handler() {}
app.all('/files/*', handler);
app.get('/users/:id', handler);
app.get('/api/orders/:id', handler);
app.all('/cdn/assets/*', handler);
