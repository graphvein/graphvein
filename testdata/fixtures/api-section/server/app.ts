import express from 'express';

const app = express();

export function listItems() {}
export function getItem() {}
export function createItem() {}
export function listUsers() {}
export function tieA() {}
export function tieB() {}
export function never() {}

app.get('/t016d-items', listItems);
app.get('/t016d-items/:id', getItem);
app.post('/t016d-items', createItem);
app.get('/t016d-api/t016d-users', listUsers);
app.get('/t016d-tie', tieA);
app.get('/t016d-tie', tieB);
app.get('/t016d-never', never);
app.get('/t016d-inline', (_req, res) => {
  res.send('inline');
});

// Never mounted: its route has no composed path.
const orphan = express.Router();
orphan.get('/t016d-orphan', never);
