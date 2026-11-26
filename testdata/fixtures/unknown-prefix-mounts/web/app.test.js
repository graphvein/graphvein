const express = require("express");

// UP-5 in a test file: the entries carry scope "test".
const h = (req, res) => res.end();
const app = express();
const probe = express.Router();
probe.get("/probe/check", h);
app.use(process.env.PROBE, probe);
