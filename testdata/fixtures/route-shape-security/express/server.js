// Serves the fixture app for the oracle (scripts/oracle/route_security).
import { app } from './app.js';

const port = Number(process.env.PORT || 54401);
app.listen(port, '127.0.0.1', () => console.log(`listening ${port}`));
