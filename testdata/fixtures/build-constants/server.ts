import express from 'express';
const app = express();
function handler() {}
app.get('/bc/users', handler);
app.get('/bc/users/:id', handler);
app.post('/bc/orders', handler);
app.get('/bc/global', handler);
app.get('/:org/settings', handler);
app.get('/bc/articles/:slug', handler);
