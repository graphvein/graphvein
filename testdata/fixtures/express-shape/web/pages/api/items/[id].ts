import type { NextApiRequest, NextApiResponse } from 'next';

// SH-E3 (path from the file name), SH-E5 (id read through req.query is
// the path parameter), SH-E34 (one route for every method).
export default async function handler(req: NextApiRequest, res: NextApiResponse) {
  if (req.method === 'GET') {
    const { id, expand } = req.query;
    return res.status(200).json({ id, expand });
  }
  if (req.method === 'PUT') {
    const token = req.headers.authorization;
    const theme = req.cookies.theme;
    const { title } = req.body;
    return res.status(200).json({ title });
  }
  res.setHeader('Allow', 'GET, PUT');
  res.status(405).end();
}
