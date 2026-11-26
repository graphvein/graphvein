import type { NextApiRequest, NextApiResponse } from 'next';

// SH-E3 catch-all (an array of strings), SH-E26 Pages redirect default 307.
export default (req: NextApiRequest, res: NextApiResponse) => {
  res.redirect('/docs');
};
