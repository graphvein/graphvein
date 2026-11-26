const express = require("express");

const router = express.Router();
router.get("/r11", (req, res) => res.json({ route: "r11" }));
exports.other = router;
