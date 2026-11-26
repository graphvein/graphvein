import express, { Router } from 'express';
export const appFactory = express;
export const routerFactory = Router;
export { Router as relayRouter };
export default () => {
  return Router();
};
