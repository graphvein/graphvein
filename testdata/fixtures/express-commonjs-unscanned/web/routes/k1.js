const express = require("express");

const router = express.Router();
router.get("/from-file", (req, res) => res.json({ route: "k1-file" }));

module.exports = router;
