import express from 'express';
const app = express();
function handler() {}
function other() {}
function auth() {}
app.get('/t012b-api/t012b-items', handler);
app.get('/t012b-api/t012b-items/:id', handler);
app.delete('/t012b-api/t012b-items', handler);
app.head('/t012b-api/t012b-items', handler);
app.options('/t012b-api/t012b-items', handler);
app.post('/t012b-api/t012b-items', handler);
app.put('/t012b-api/t012b-items', handler);
app.patch('/t012b-api/t012b-items', handler);
app.get('/t012b-same', auth, handler);
app.get('/t012b-same', auth, handler);
app.get('/t012b-handlers', handler);
app.get('/t012b-handlers', other);
app.get('/t012b-middleware', handler);
app.get('/t012b-middleware', auth, handler);
const one = express.Router();
const two = express.Router();
app.use(one);
app.use(two);
one.get('/t012b-routers', handler);
two.get('/t012b-routers', handler);
app.all('/t012b-anysame', handler);
app.all('/t012b-anysame', handler);
app.get('/t012b-anymix', handler);
app.all('/t012b-anymix', handler);
