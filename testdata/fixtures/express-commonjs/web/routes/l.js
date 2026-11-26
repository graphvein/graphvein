const express = require("express");
const { reply } = require("../reply");

// C12: a factory exported as a member of an object literal, called with an
// argument at the mount.
function makeItems(label) {
  // label names the router in logs; it does not change the routes.
  const router = express.Router();
  router.get("/o", reply("l-o"));
  return router;
}

module.exports = { makeItems };
