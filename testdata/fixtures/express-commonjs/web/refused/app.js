// Mounts no row reads (T036): each router below keeps no path. This app is
// not served by the oracle (Express throws at some of these mounts).
const express = require("express");

const app = express();

// R1: a specifier that is not a string literal.
app.use("/r1", require(process.env.ROUTES_MODULE));

// R2: a conditional require.
app.use("/r2", process.env.FLAG ? require("./r2a") : require("./r2b"));

// R3: `module.exports` written inside a branch.
app.use("/r3", require("./r3"));

// R4: `module.exports` written twice.
app.use("/r4", require("./r4"));

// R5: an ES module: `require` returns its namespace object, not the router.
app.use("/r5", require("./r5"));

// R6: `exports = router` rebinds the local name only.
app.use("/r6", require("./r6"));

// R7: the exports object itself (`module.exports = { router }`), not its
// router.
app.use("/r7", require("./r7"));

// R8: a module that does not exist.
app.use("/r8", require("./missing"));

// R9: a TypeScript module required from JavaScript (Node loads no .ts
// file for an extensionless require).
app.use("/r9", require("./r9"));

// R10: exports written through Object.assign.
app.use("/r10", require("./r10").router);

// R11: a destructured name with a default value.
const { router: r11 = express.Router() } = require("./r11");
app.use("/r11", r11);

module.exports = app;
