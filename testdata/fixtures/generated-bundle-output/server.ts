import express from 'express';
const app = express();
export function handler() {}
app.get('/t035-gb/items', handler);
app.get('/t035-gb/items/:id', handler);
app.listen(3000);
