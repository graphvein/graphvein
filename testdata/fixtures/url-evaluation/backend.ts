import express from 'express';
const app = express();
function handler() {}
app.get('/api/dot', handler);
app.get('/api/a', handler);
app.get('/api/b', handler);
app.get('/api/items/a/b/c', handler);
app.get('/api/items/:id', handler);
app.get('/api/items', handler);
app.get('/api/coerce/undefined/null/false/12', handler);
app.post('/api/express', handler);
app.post('/api/express/', handler);
