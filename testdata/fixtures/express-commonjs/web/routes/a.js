const express = require("express");
const { reply } = require("../reply");

const router = express.Router();
router.get("/x", reply("a-x"));
router.post("/items/:id", reply("a-item"));

function listItems(req, res) {
  res.json({ route: "a-list" });
}
router.get("/list", listItems);

module.exports = router;
