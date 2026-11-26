const express = require("express");
const { reply } = require("../reply");

const router = express.Router();
router.get("/r", reply("i-r"));

module.exports = router;
