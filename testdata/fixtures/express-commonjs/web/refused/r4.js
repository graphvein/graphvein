const express = require("express");

const router = express.Router();
router.get("/r4", (req, res) => res.json({ route: "r4" }));
module.exports = router;
module.exports = express.Router();
