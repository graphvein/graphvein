import express from "express";

const router = express.Router();
router.get("/r9", (req, res) => res.json({ route: "r9" }));
module.exports = router;
