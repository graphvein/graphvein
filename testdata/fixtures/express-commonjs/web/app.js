const express = require("express");
const { reply } = require("./reply");

const app = express();
app.get("/health", reply("health"));

// C1: a required module used directly; it exports the router with
// `module.exports = router`.
app.use("/a", require("./routes/a"));

// C2: the same, bound to a const first.
const bRouter = require("./routes/b");
app.use("/b", bRouter);

// C3: `module.exports = { router }`, read as a member of the require.
app.use("/c", require("./routes/c").router);

// C4: `exports.router = router`, destructured with a rename.
const { router: dRouter } = require("./routes/d");
app.use("/d", dRouter);

// C5: `module.exports.items = router`, destructured shorthand.
const { items } = require("./routes/e");
app.use("/e", items);

// C6: a directory required through its index.js.
app.use("/f", require("./routes/f"));

// C7: a factory function the module exports; the router it returns.
const makeRouter = require("./routes/g");
app.use("/g", makeRouter());

// C8: a mount inside an `if`: the composed path is conditional.
if (process.env.ADMIN) {
  app.use("/h", require("./routes/h"));
}

// C9: a module re-exporting another module's router.
app.use("/i", require("./routes/i"));

// C10: a prefix read from the environment: no path, the hop is stored.
app.use(process.env.J_PREFIX, require("./routes/j"));

// C11: a router without a prefix.
app.use(require("./routes/k"));

// C12: a factory function exported in an object literal, destructured.
const { makeItems } = require("./routes/l");
app.use("/l", makeItems("items"));

module.exports = app;
