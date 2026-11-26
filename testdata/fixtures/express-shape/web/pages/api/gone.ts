import type { NextApiRequest, NextApiResponse } from 'next';

// SH-E23: Next.js sends no Content-Type for 204.
export default function gone(req: NextApiRequest, res: NextApiResponse) {
  return res.status(204).json({ gone: true });
}
