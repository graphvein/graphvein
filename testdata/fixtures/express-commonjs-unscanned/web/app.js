const express = require("express");

const app = express();

// U1: routes/u1.js (ignored by .gitignore) comes before routes/u1/index.js
// in Node's loader order: Node loads the unscanned file.
app.use("/u1", require("./routes/u1"));
// U2: the same with routes/u2.js excluded by .graphveinignore.
app.use("/u2", require("./routes/u2"));
// U3: the extensionless file vendor (a name the scanner always skips)
// comes before vendor.js.
app.use("/u3", require("./vendor"));
// K1 (control): routes/k1.js is scanned and comes first; the ignored
// directory routes/k1/ is never reached.
app.use("/k1", require("./routes/k1"));

module.exports = app;
