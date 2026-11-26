import express from 'express';
import { getItem } from './handlers';

const router = express.Router();
router.get('/items/:id', getItem);
export default router;
