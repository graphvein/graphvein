import express from 'express';
import { listItems, report } from './handlers';
import { stable } from './stable';
import router from './router';

const app = express();
app.get('/api/items', listItems);
app.use('/api/v2', router);
app.get('/api/report', report);
// Stable endpoints follow.
// Nothing below this line changed.
app.get('/api/stable', stable);
app.listen(3000);
