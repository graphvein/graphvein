import express from 'express';
const app = express();
export function handler() {}
app.get('/api/items', handler);
app.post('/api/items', handler);
app.put('/api/items', handler);
app.patch('/api/items', handler);
app.delete('/api/items', handler);
app.head('/api/items', handler);
app.options('/api/items', handler);
app.get('/api/items/fixed', handler);
app.get('/api/items/:id', handler);
app.get('/api/optional/:id?', handler);
app.get('/api/wild/*', handler);
app.all('/api/any', handler);
app.get('/api/express-head', handler);
app.get('/unique/resource', handler);
app.get('/api/tie', handler);
app.get('/api/tie', handler2);
app.get('/api/tie', handler3);
app.get('/api/tie', handler4);
app.get('/api/tie', handler5);
app.get('/api/tie', handler6);
app.get('/api/tie', handler7);
app.get('/api/tie', handler8);
app.get('/api/tie', handler9);
app.get('/api/tie', handler10);
app.get('/api/tie', handler11);
app.get('/api/tie', handler12);
function handler2() {}
function handler3() {}
function handler4() {}
function handler5() {}
function handler6() {}
function handler7() {}
function handler8() {}
function handler9() {}
function handler10() {}
function handler11() {}
function handler12() {}
