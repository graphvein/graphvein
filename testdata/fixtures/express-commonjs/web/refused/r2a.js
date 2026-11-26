const express = require("express");

const router = express.Router();
router.get("/r2a", (req, res) => res.json({ route: "r2a" }));
module.exports = router;
