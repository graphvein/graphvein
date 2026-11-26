const express = require("express");
const { reply } = require("../reply");

const router = express.Router();
router.get("/k/p", reply("k-p"));

module.exports = router;
