const express = require("express");

const router = express.Router();
router.get("/r3", (req, res) => res.json({ route: "r3" }));
if (process.env.R3) {
  module.exports = router;
}
