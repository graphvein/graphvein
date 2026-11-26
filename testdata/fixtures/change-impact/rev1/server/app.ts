import express from 'express';
import { listItems, dropLegacy, report } from './handlers';
import { stable } from './stable';
import router from './router';

const app = express();
app.get('/api/items', listItems);
app.use('/api', router);
app.delete('/api/legacy', dropLegacy);
app.get('/api/report', report);
app.get('/api/stable', stable);
app.listen(3000);
