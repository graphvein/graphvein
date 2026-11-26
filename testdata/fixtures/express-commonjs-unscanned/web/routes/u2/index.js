const express = require("express");

const router = express.Router();
router.get("/from-index", (req, res) => res.json({ route: "u2-index" }));

module.exports = router;
