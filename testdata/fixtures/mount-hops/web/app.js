const express = require("express");

const list = (req, res) => res.end();
const app = express();

const api = express.Router();
api.get("/", list);
api.get("/items", list);
api.get("/items/:id", list);

const base = process.env.BASE || "/api";
app.use(base, api);
app.get("/health", list);
// Read from code: a value placing api's /items/:id here is refused.
app.get("/api/items/:anything", list);

const admin = express.Router();
admin.get("/stats", list);
app.use(process.env.ADMIN, admin);

// A hop below a constant prefix: /x is read and composed above it.
const outer = express.Router();
const inner = express.Router();
inner.get("/deep", list);
outer.use(process.env.INNER, inner);
app.use("/x", outer);

app.listen(3000);
