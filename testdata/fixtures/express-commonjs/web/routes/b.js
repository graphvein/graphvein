const { Router } = require("express");
const { reply } = require("../reply");

const router = Router();
router.get("/y", reply("b-y"));

module.exports = router;
