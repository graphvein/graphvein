const express = require("express");

const router = express.Router();
router.get("/from-file", (req, res) => res.json({ route: "u1-file" }));

module.exports = router;
