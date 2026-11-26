import express from 'express';
const app = express();
app.get('/t011g/config', (_req, res) => res.send('test'));
