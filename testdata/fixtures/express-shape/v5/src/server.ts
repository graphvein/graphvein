import express from 'express';
import Joi from 'joi';
import { z } from 'zod';

// Express 5: the body parsers leave req.body undefined without a body of
// their type.
export const app5 = express();
app5.use(express.json());

const JPlain5 = Joi.object({ a: Joi.string().required() });
const JRoot5 = Joi.object({ a: Joi.string().required() }).required();
const Item5 = z.object({ name: z.string() });

// SH-E14: a Joi root object that is not required() accepts no body.
app5.post('/q1', (req, res) => {
  const { error } = JPlain5.validate(req.body);
  if (error) {
    return res.status(400).json({ bad: true });
  }
  res.status(201).json({ ok: true });
});

// SH-E14: required() at the root rejects a missing body.
app5.post('/q2', (req, res) => {
  const { error } = JRoot5.validate(req.body);
  if (error) {
    return res.status(400).json({ bad: true });
  }
  res.status(201).json({ ok: true });
});

// SH-E12: zod rejects undefined.
app5.post('/q3', (req, res) => {
  const v = Item5.parse(req.body);
  res.status(201).json({ name: v.name });
});
