const express = require("express");
const { reply } = require("../reply");

const router = express.Router();
router.get("/s", reply("h-s"));

module.exports = router;
