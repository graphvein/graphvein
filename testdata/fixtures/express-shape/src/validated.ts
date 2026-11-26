import express from 'express';
import * as OpenApiValidator from 'express-openapi-validator';

// SH-E32: a global OpenAPI validator decides the request shape (H9).
export const api = express();
api.use(express.json());
api.use(OpenApiValidator.middleware({ apiSpec: './openapi.yaml' }));

api.post('/pets', (req, res) => {
  res.status(201).json({ name: req.body.name });
});
