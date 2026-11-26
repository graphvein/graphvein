import express from 'express';
const app = express();
const api = express.Router();
export function handler() {}
api.get('/t034-genres/movie', handler);
api.get('/t034-genres/tv', handler);
api.get('/t034-items/featured', handler);
api.get('/t034-items/:id', handler);
api.delete('/t034-items/:id', handler);
app.use('/t034-api', api);
