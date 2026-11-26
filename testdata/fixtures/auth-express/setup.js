import { trace } from './mw.js';

// configure receives the app and is called once with it at the top level
// of app.js: its use is read at the call (DA-E3).
export function configure(app) {
  app.use(trace('configured'));
}

// An object-pattern parameter, a guarded use and a use after an early
// return (DA-E3: conditional registration).
export const configureAll = ({ app, skip = true }) => {
  app.use(trace('cfg-first'));
  if (process.env.GRAPHVEIN_ORACLE_FLAG) {
    app.use(trace('cfg-flag'));
  }
  if (skip === true) {
    return;
  }
  app.use(trace('cfg-after-return'));
};

// Called twice with the same app: not read (escape).
export function configureTwice(app) {
  app.use(trace('twice'));
}

// Async: its body may run after the call returns (escape).
export async function configureAsync(app) {
  await null;
  app.use(trace('async'));
}

// The parameter bound to another name, or passed on: not read (escape).
export function configureAlias(app) {
  const target = app;
  target.use(trace('alias'));
}

export function configurePass(app) {
  configure(app);
}
