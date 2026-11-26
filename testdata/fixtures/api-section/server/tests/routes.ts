import express from 'express';

const app = express();

export function testOnly() {}

app.get('/t016d-test-only', testOnly);
