const express = require("express");
const { reply } = require("../reply");

const router = express.Router();
router.get("/z", reply("c-z"));

module.exports = { router };
