import express from 'express';
export const app = express();
export const shared = express.Router();
shared.get('/shared', (_req, res) => res.send('shared'));
app.use('/t011g/prod-express', shared);
