const express = require("express");
const { reply } = require("../reply");

const router = express.Router();
router.get("/w", reply("d-w"));

exports.router = router;
