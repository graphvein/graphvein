const express = require("express");
const { reply } = require("../reply");

function makeRouter() {
  const router = express.Router();
  router.get("/t", reply("g-t"));
  return router;
}

module.exports = makeRouter;
