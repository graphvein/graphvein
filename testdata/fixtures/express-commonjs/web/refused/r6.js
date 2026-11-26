const express = require("express");

const router = express.Router();
router.get("/r6", (req, res) => res.json({ route: "r6" }));
// eslint-disable-next-line no-global-assign
exports = router;
