import express from 'express';
import { JPlain } from './schemas';

export const plain = express.Router();

plain.post('/echo', (req, res) => {
  const note = req.body?.note;
  res.json({ note });
});

plain.get('/cookie', (req, res) => {
  res.send(req.cookies?.sid);
});

// SH-E14: no body parser is mounted before this router: req.body is
// undefined, which a Joi root object that is not required() accepts.
plain.post('/joi', (req, res) => {
  const { error } = JPlain.validate(req.body);
  if (error) {
    return res.status(400).json({ bad: true });
  }
  res.status(201).json({ ok: true });
});
