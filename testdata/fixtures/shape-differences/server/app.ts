import express from 'express';
import Joi from 'joi';

export const app = express();
app.use(express.json());

const articleBody = Joi.object({
  article: Joi.object({
    title: Joi.string().required(),
    body: Joi.string().required(),
    tags: Joi.array(),
  }).required(),
}).required();

// SD-6: a Joi validation the handler tests: the body is a fact.
app.post('/w4/articles', (req, res) => {
  const { error } = articleBody.validate(req.body);
  if (error) {
    return res.status(422).json({ message: 'invalid' });
  }
  res.status(201).json({ ok: true });
});

// D: names read in the handler (partial).
app.post('/w4/comments', (req, res) => {
  res.status(201).json({ text: req.body.text });
});
