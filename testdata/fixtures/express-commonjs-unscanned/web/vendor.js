const express = require("express");

const router = express.Router();
router.get("/from-vendor-js", (req, res) => res.json({ route: "u3-vendor-js" }));

module.exports = router;
