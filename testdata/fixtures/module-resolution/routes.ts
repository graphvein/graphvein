import express from 'express';
import { Router } from 'express';
import { a } from './barrel';
import { ns } from './barrel';
export const router = Router();
router.get('/resolved', a);
router.get('/namespace', ns.b);
export const prefix = '/mounted';
export const app = express();
