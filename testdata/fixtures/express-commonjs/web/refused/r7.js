const express = require("express");

const router = express.Router();
router.get("/r7", (req, res) => res.json({ route: "r7" }));
module.exports = { router };
