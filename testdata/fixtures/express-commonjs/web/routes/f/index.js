const express = require("express");
const { reply } = require("../../reply");

const router = express.Router();
router.get("/u", reply("f-u"));

module.exports = router;
