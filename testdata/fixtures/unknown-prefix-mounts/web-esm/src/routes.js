import express from "express";

// UP-9 per scope: the router production mounts below a
// configured base, and a test harness mounts at "/t".
export const router = express.Router();
router.get("/items/list", (req, res) => res.json([]));
