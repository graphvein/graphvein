import { app, child, prefix, Router } from './route-barrel';
app.use(prefix, child);
const router = Router();
app.use('/factory', router);
router.get('/relay', () => {});
