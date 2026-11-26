// A repository auth middleware: no row reads it (auth as written).
export function requireUser(req, res, next) {
  if (req.headers['x-user'] !== 'fixture') {
    res.status(401).json({ error: 'unauthorized' });
    return;
  }
  next();
}
