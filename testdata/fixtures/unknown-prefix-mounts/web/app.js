const express = require("express");

const h = (req, res) => res.end();
const app = express();
const config = { base: process.env.BASE };

// UP-5: a router mounted at a prefix read from configuration.
const api = express.Router();
api.get("/", h);
api.get("/users", h);
api.get("/users/:id", h);
api.post("/users/:id/avatar", h);
// A read prefix below the hop composes into the paths below it.
const v1 = express.Router();
v1.get("/status", h);
api.use("/v1", v1);
app.use(config.base, api);

// UP-5 below the read prefix /x: the entries carry above "/x".
const outer = express.Router();
const inner = express.Router();
inner.get("/deep/path", h);
outer.use(process.env.INNER, inner);
app.use("/x", outer);

// UP-5, conditional: a mount inside an if.
const beta = express.Router();
beta.get("/beta/feature", h);
if (process.env.BETA) {
  app.use(process.env.BETA_BASE, beta);
}

// UP-8: one router at two unknown hops; one entry, the first hop's key.
const twice = express.Router();
twice.get("/twice/here", h);
app.use(process.env.ONE, twice);
app.use(process.env.TWO, twice);

// UP-9: a router mounted at a read prefix and at an unknown one keeps its
// known path and gets no entry.
const shared = express.Router();
shared.get("/shared/item", h);
app.use("/known", shared);
app.use(process.env.SHARED, shared);

// D (unread_pattern): a pattern no Express 4 row reads, below a hop.
const files = express.Router();
files.get("/files/:file.:ext", h);
files.get("/files/list", h);
app.use(process.env.FILES, files);

// D (unread_pattern): a router mounted only below a prefix no row reads.
const legacy = express.Router();
legacy.get("/legacy/page", h);
app.use("/old*", legacy);

// UP-10: a router nothing mounts.
const orphan = express.Router();
orphan.get("/orphan/page", h);

app.get("/health", h);
app.listen(3000);
