const express = require("express");

const router = express.Router();
router.get("/r2b", (req, res) => res.json({ route: "r2b" }));
module.exports = router;
