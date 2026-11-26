import express from "express";

const router = express.Router();
router.get("/r5", (req, res) => res.json({ route: "r5" }));
export default router;
