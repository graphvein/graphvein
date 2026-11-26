// Oracle-only server for the route-shape-security Express app: mounts
// express-openid-connect's auth() (authRequired false, so it refuses
// nothing) in front of the fixture app, as requiresAuth needs, and serves
// it on 127.0.0.1. Kept out of the fixture so the indexed app is unchanged.
import express from 'express';
import oidc from 'express-openid-connect';
import { app, SECRET } from './app.js';

const port = Number(process.env.PORT || 54401);
const outer = express();
outer.use(oidc.auth({
  authRequired: false,
  errorOnRequiredAuth: true,
  secret: SECRET + SECRET,
  baseURL: `http://127.0.0.1:${port}`,
  clientID: 'fixture',
  issuerBaseURL: 'https://issuer.example',
  routes: { login: false, logout: false, callback: false },
}));
outer.use(app);
outer.listen(port, '127.0.0.1', () => console.log(`listening ${port}`));
