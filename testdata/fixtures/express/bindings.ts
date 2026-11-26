import create, { appFactory, routerFactory, relayRouter } from './factories';
import { handle } from './handlers';
const app = appFactory();
const otherRoot = appFactory();
const router = create();
app.use('/returned', router);
otherRoot.use('/returned', router);
router.get('/default', handle);
const named = routerFactory();
const PREFIX = '/named';
app.use(PREFIX, named);
named.get('/alias', handle);
app.get('/repeat', () => { app.get('/repeat', handle); });
function build() { return routerFactory(); }
const local = build();
app.use('/local', local);
local.get('/factory', handle);
let mutated = routerFactory();
mutated = client;
mutated.get('/not-a-route', handle);
app.get(/regex/, handle);
app.get(['/array'], handle);
app.get('/concat' + suffix, handle);
app.get(`/template/${suffix}`, handle);
function duplicate() {}
function duplicate() {}
app.get('/ambiguous', duplicate);
const relayed = relayRouter();
relayed.get('/not-resolved-through-reexport', handle);
