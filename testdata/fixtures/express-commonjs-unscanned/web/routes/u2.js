const express = require("express");

const router = express.Router();
router.get("/from-file", (req, res) => res.json({ route: "u2-file" }));

module.exports = router;
