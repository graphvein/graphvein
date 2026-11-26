import { z } from 'zod';
import Joi from 'joi';

export const CreateUser = z.object({
  name: z.string().min(1),
  email: z.string().email().optional(),
  age: z.number().int().nullable(),
  tags: z.array(z.string()).default([]),
});

export const Login = z.object({
  user: z.string(),
  password: z.string().min(8),
}).strict();

export const Profile = z.object({
  bio: z.string().max(200),
});

export const articleSchema = Joi.object({
  article: Joi.object({
    title: Joi.string().required(),
    tags: Joi.array().items(Joi.string()).empty(null).default([]),
    draft: Joi.boolean(),
  }).required(),
}).required();

export const listQuery = Joi.object({
  limit: Joi.number().integer().default(20),
  author: Joi.string(),
});

// Further probes (src/probes.ts).
export const Item = z.object({ name: z.string(), qty: z.number() });

// Joi converts strings to numbers and booleans unless strict().
export const JNum = Joi.object({ n: Joi.number().required(), b: Joi.boolean().required(), s: Joi.string().required() });
export const JStrict = Joi.object({ n: Joi.number().strict().required(), b: Joi.boolean().required() });
export const JStrictAll = Joi.object({ n: Joi.number().integer().required(), b: Joi.boolean() }).strict();

// The last presence modifier wins; empty(null); insensitive().
export const JOrder = Joi.object({ a: Joi.string().required().optional(), c: Joi.string().required().default('x'), d: Joi.string().optional().required() });
export const JEmpty = Joi.object({ s: Joi.string().required().empty(null), t: Joi.string().empty(null) });
export const JIns = Joi.object({ k: Joi.string().valid('a').insensitive().required(), m: Joi.string().valid('b').required() });

// A Joi root object that is not required() accepts a missing body.
export const JPlain = Joi.object({ a: Joi.string().required() });
