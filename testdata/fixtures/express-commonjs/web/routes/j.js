const express = require("express");
const { reply } = require("../reply");

const router = express.Router();
router.get("/q", reply("j-q"));

module.exports = router;
