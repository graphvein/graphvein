import express from 'express';
const app = express();
function handle(req, res) { res.json({}); }
app.get('/t015b-requests', handle);
app.post('/t015b-requests', handle);
app.patch('/t015b-requests', handle);
app.delete('/t015b-requests', handle);
app.get('/t015b-requests/:id', handle);
