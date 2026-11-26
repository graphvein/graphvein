import express from 'express';
const app = express();
function handler() {}
app.get('/t012e/articles/:slug', handler);
app.get('/t012e/articles', handler);
app.post('/t012e/articles', handler);
app.get('/t012e/scanned', handler);
app.get('/t012e/url/assigned', handler);
app.get('/t012e/url/literal', handler);
app.get('/t012e/url/alias', handler);
app.get('/t012e/url/host', handler);
app.get('/t012e/url/module', handler);
app.get('/t012e/t012e/articles/:slug', handler);
