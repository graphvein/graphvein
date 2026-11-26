import express from "express";

import { standalone } from "../app.js";

// X10: a test-scope mount of a production app does not consume it.
export const harness = express();
harness.use("/t", standalone);

// X16: a test serving a production app does not make it a root.
import { testServed } from "../app.js";
testServed.listen(0);
