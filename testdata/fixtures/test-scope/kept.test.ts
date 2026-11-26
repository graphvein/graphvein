import express from 'express';
const app = express();
app.get('/t011g/excluded', (_req, res) => res.send('production'));
