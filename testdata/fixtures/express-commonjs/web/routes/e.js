const express = require("express");
const { reply } = require("../reply");

const items = express.Router();
items.get("/v", reply("e-v"));

module.exports.items = items;
