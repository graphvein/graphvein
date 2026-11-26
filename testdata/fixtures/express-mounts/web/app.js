import express from "express";

const reply = (name) => (req, res) => res.json({ route: name });

export const app = express();
app.get("/health", reply("health"));

// X1: app.use with a constant prefix. The sub-app answers only below the
// mount: /sub/sub/ping, never /sub/ping.
export const sub = express();
sub.get("/sub/ping", reply("sub-ping"));
app.use("/sub", sub);

// X2: a prefix read from the environment: no path, the hop is stored in
// unresolved_mounts.
export const env = express();
env.get("/env/ping", reply("env-ping"));
app.use(process.env.SUB, env);

// X3: a router used inside the mounted sub-app.
export const inner = express();
const items = express.Router();
items.get("/x", reply("inner-x"));
inner.use("/inc", items);
app.use("/m", inner);

// X4: nested sub-apps.
export const mid = express();
export const leaf = express();
leaf.get("/leaf", reply("leaf"));
mid.use("/b", leaf);
app.use("/a", mid);

// X5: a conditional mount.
export const admin = express();
admin.get("/panel", reply("panel"));
if (process.env.ADMIN) {
  app.use("/admin", admin);
}

// X6: a sub-app mounted through a router, the router mounted on the app.
export const viaRouter = express();
viaRouter.get("/vr", reply("via-router"));
const outerRouter = express.Router();
outerRouter.use("/in", viaRouter);
app.use("/out", outerRouter);

// X7: a sub-app mounted without a prefix: its paths are the same through
// the mount.
export const flat = express();
flat.get("/flat", reply("flat"));
app.use(flat);

// X8: a sub-app mounted at several prefixes (an array is not read): no
// path, the hop is stored.
export const multi = express();
multi.get("/multi", reply("multi"));
app.use(["/p1", "/p2"], multi);

// X9: a sub-app mounted by an object the linker does not read: no path.
export const raw = express();
raw.get("/raw", reply("raw"));
const holder = { server: express.Router() };
holder.server.use("/raw", raw);

// X10: an app only a test mounts keeps its own root.
export const standalone = express();
standalone.get("/alone", reply("alone"));

// X11: a route added after a mount at its path: Express passes a request
// the mounted app does not answer on, so /sh/b is the parent's; /sh/a is
// answered by the sub-app's route first (DA-E1's earlier route handler,
// recorded in mounted_middleware_unknown).
export const shadowSub = express();
shadowSub.get("/a", reply("shadow-sub-a"));
app.use("/sh", shadowSub);
app.get("/sh/a", reply("shadow-parent-a"));
app.get("/sh/b", reply("shadow-parent-b"));

// X12-X15: apps mounted here and also served on their own (serve.js).
export const served = express();
served.get("/own", reply("served-own"));
app.use("/srv", served);

export const created = express();
created.get("/created", reply("created"));
app.use("/cr", created);

export const bound = express();
bound.get("/bound", reply("bound"));
app.use("/bd", bound);

export const later = express();
later.get("/later", reply("later"));
app.use("/lt", later);

// X16: an app mounted here and served only by a test: no own root.
export const testServed = express();
testServed.get("/test-served", reply("test-served"));
app.use("/ts", testServed);

// X17 (RO-V2): a sub-app whose own stack ends with a catch-all
// middleware is mounted before a parent route below its prefix: the
// sub-app answers /z/b with its 404 first. /z/b is kept, conditional,
// with assumption RO-V2 naming the mount.
export const catchSub = express();
catchSub.get("/a", reply("catch-sub-a"));
catchSub.use((req, res) => res.status(404).json({ error: "sub-404" }));
app.use("/z", catchSub);
app.get("/z/b", reply("catch-parent-b"));

// X18 (RO-V2): the same with a router and a 404 middleware.
const notFound = (req, res) => res.status(404).json({ error: "router-404" });
const statusRouter = express.Router();
statusRouter.get("/users/list", reply("status-users"));
statusRouter.use(notFound);
app.use("/api", statusRouter);
app.get("/api/status/now", reply("status-now"));

// X19 (control): a parent route outside the mount's prefix, and one added
// before the mount, keep their paths unconditional.
app.get("/zz/c", reply("catch-outside"));
app.get("/zz/*", reply("catch-outside-wild"));
