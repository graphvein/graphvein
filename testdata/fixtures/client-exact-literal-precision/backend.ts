import express from 'express';
const app = express();
export function handler() {}
app.get('/t017b-users/:id/t017b-edit', handler);
app.get('/t017b-api/:tenant/t017b-reports', handler);
app.get('/t017b-api/t017b-items', handler);
app.get('/:org/t017b-settings', handler);
