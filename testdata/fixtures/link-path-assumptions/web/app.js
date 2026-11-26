import express from "express";

const reply = (name) => (req, res) => res.json({ route: name });

export const app = express();

// A1 (RO-V2): a router whose own stack ends with a 404 middleware is
// mounted before a parent route below its prefix: /z/b is conditional,
// with assumption RO-V2.
const notFound = (req, res) => res.status(404).json({ error: "router-404" });
const catchRouter = express.Router();
catchRouter.get("/a", reply("catch-a"));
catchRouter.use(notFound);
app.use("/z", catchRouter);
app.get("/z/b", reply("parent-b"));

// A2: one router mounted twice: below /y after a catch-all router (RO-V2
// on /y/c) and below /w with nothing before it (/w/c plain).
const yCatch = express.Router();
yCatch.use(notFound);
app.use("/y", yCatch);
const shared = express.Router();
shared.get("/c", reply("shared-c"));
app.use("/y", shared);
app.use("/w", shared);

// A3 (control): a route nothing is mounted before.
app.get("/plain/one", reply("plain-one"));
