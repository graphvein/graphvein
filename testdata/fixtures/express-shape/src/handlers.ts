import { Request, RequestHandler, Response } from 'express';
import { articleSchema, listQuery } from './schemas';
import { ArticleBody, ArticleQuery, NewComment } from './types';
import { addComment } from './services';

// SH-E14 (Joi validate, error tested), SH-E17 (RequestHandler generics),
// SH-E29 (throw).
export const createArticle: RequestHandler<unknown, unknown, ArticleBody, unknown> = (req, res) => {
  const { value, error } = articleSchema.validate(req.body);
  if (error) {
    throw new Error('invalid');
  }
  res.status(201).json({ article: value.article });
};

// SH-E14 on the query, SH-E17 Query generic, SH-E4 destructuring.
export const listArticles: RequestHandler<unknown, unknown, unknown, ArticleQuery> = (req, res) => {
  const result = listQuery.validate(req.query);
  if (result.error) {
    return res.status(422).json({ message: result.error.message });
  }
  const { limit, offset } = req.query;
  res.json({ limit, offset });
};

// SH-E17 Request<P, ResBody, ReqBody> on the parameter; SH-E18 typed callee.
export function postComment(req: Request<{ slug: string }, unknown, NewComment>, res: Response) {
  const saved = addComment(req.body);
  res.status(201).json(saved);
}

// SH-E18 `as T`; Joi validate with options (presence and keys unknown).
export function postReview(req: Request, res: Response) {
  const input = req.body as NewComment;
  const { error } = articleSchema.validate(req.body, { allowUnknown: true });
  if (error) {
    return res.sendStatus(400);
  }
  res.send(input.body);
}
