const express = require("express");

const router = express.Router();
router.get("/r10", (req, res) => res.json({ route: "r10" }));
Object.assign(module.exports, { router });
