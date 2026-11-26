import express from "express";
import { router } from "../src/routes.js";

// A test harness mounts the production router at a known prefix: its
// test-scope path does not remove the production entry.
const harness = express();
harness.use("/t", router);
