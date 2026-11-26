// Serving forms: each app below is mounted in app.js and also
// served at its own root here. The oracle serves the apps itself and does
// not import this file.
import http from "node:http";
import https from "https";

import { bound, created, later, served } from "./app.js";

// X12: x.listen(...).
served.listen(3001);

// X13: http.createServer(x).listen(...).
http.createServer(created).listen(3002);

// X14: a server bound to a name, then listened on.
const server = https.createServer({}, bound);
server.listen(3003);

// X15: inside a function: the own-root path is conditional.
export function start() {
  later.listen(3004);
}
