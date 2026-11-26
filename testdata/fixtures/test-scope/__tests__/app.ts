import express from 'express';
const app = express();
app.get('/t011g/express', (_req, res) => res.send('test'));
import { app as production, shared } from '../express';
production.use('/t011g/test-express', shared);
