import express from 'express';

const app = express();
app.get('/users/:id', (req, res) => res.json({ id: req.params.id }));
app.post('/users', (req, res) => res.json({}));
app.delete('/users/:id', (req, res) => res.end());
app.all('/any', (req, res) => res.end());
app.get('/files/*', (req, res) => res.end());
app.get('/opt/:x?', (req, res) => res.end());
app.get('/slash/', (req, res) => res.end());
app.listen(3000, () => console.log('listening'));
