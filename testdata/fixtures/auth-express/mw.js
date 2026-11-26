// Middleware the oracle runs: each records its name as written at the
// registration site, so a request's trace lists the middleware that ran.
const seen = (req, label) => {
  req.trace = req.trace || [];
  req.trace.push(label);
};

export function logger(req, res, next) {
  seen(req, 'logger');
  next();
}

export function audit(req, res, next) {
  seen(req, 'audit');
  next();
}

export function requireAdmin(req, res, next) {
  seen(req, 'requireAdmin');
  next();
}

export function trace(label) {
  return function traced(req, res, next) {
    seen(req, "trace('" + label + "')");
    next();
  };
}

export function reply(label) {
  return function replied(req, res) {
    res.json({ route: "reply('" + label + "')", trace: req.trace || [] });
  };
}

export const stack = [trace('spread')];

export function pathOf() {
  return '/computed';
}

// An error handler (four parameters): Express runs it only when an error
// is passed (DA-E5); it records its name when it does.
export function onError(err, req, res, next) {
  seen(req, 'onError');
  next(err);
}
